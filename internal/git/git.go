// Package git reads repository state and captures checkpoints for T3Go.
package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// ErrNotRepo means dir is not inside a Git checkout.
var ErrNotRepo = errors.New("not a git repository")

func run(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr == "" {
			stderr = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsRepo reports whether dir is in a work tree.
func IsRepo(ctx context.Context, dir string) bool {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return false
	}
	out, err := run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// Root returns the work tree's root.
func Root(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", ErrNotRepo
	}
	return out, nil
}

// Status reads the checkout's branch and cleanliness.
func Status(ctx context.Context, dir string) proto.GitStatus {
	if !IsRepo(ctx, dir) {
		return proto.GitStatus{Error: "not a git repository"}
	}
	root, err := Root(ctx, dir)
	if err != nil {
		return proto.GitStatus{Error: err.Error()}
	}
	st := proto.GitStatus{Repo: true, Root: root}
	branch, err := run(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil && branch != "" {
		st.Branch = branch
	} else {
		st.Detached = true
		if short, err := run(ctx, dir, "rev-parse", "--short", "HEAD"); err == nil {
			st.Branch = short
		}
	}
	if porcelain, err := run(ctx, dir, "status", "--porcelain"); err == nil {
		st.Dirty = porcelain != ""
	}
	if counts, err := run(ctx, dir, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
		fields := strings.Fields(counts)
		if len(fields) == 2 {
			st.Ahead, _ = strconv.Atoi(fields[0])
			st.Behind, _ = strconv.Atoi(fields[1])
		}
	}
	return st
}

// Checkpoint captures the checkout's state in a hidden ref without
// changing the user's branch or index. It returns the ref's name.
//
// `git stash create` records tracked modifications in a commit and leaves
// the work tree alone; untracked files are not part of the snapshot.
func Checkpoint(ctx context.Context, dir, threadID, runID string) (string, error) {
	if !IsRepo(ctx, dir) {
		return "", ErrNotRepo
	}
	commit, err := run(ctx, dir, "stash", "create", "t3go checkpoint")
	if err != nil {
		return "", err
	}
	if commit == "" {
		commit, err = run(ctx, dir, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
	}
	ref := "refs/t3go/checkpoints/" + threadID + "/" + runID
	if _, err := run(ctx, dir, "update-ref", ref, commit); err != nil {
		return "", err
	}
	return ref, nil
}

// DiffStat shows what changed since ref.
func DiffStat(ctx context.Context, dir, ref string) string {
	if ref == "" {
		return ""
	}
	out, err := run(ctx, dir, "diff", "--stat", ref)
	if err != nil {
		return ""
	}
	return out
}

// Diff shows what changed since ref.
func Diff(ctx context.Context, dir, ref string) (string, error) {
	if ref == "" {
		return "", errors.New("no checkpoint")
	}
	return run(ctx, dir, "diff", ref)
}

// Branches lists local branch names.
func Branches(ctx context.Context, dir string) ([]string, error) {
	out, err := run(ctx, dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// CreateWorktree adds a worktree at path on branch, from base. An existing
// branch is checked out; otherwise it is created.
func CreateWorktree(ctx context.Context, dir, path, branch, base string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("worktree path already exists: %s", path)
	}
	exists := false
	if branches, err := Branches(ctx, dir); err == nil {
		for _, b := range branches {
			if b == branch {
				exists = true
				break
			}
		}
	}
	args := []string{"worktree", "add"}
	if !exists {
		args = append(args, "-b", branch)
	}
	args = append(args, path)
	if !exists {
		if base == "" {
			base = "HEAD"
		}
		args = append(args, base)
	} else {
		args = append(args, branch)
	}
	_, err := run(ctx, dir, args...)
	return err
}

// RemoveWorktree removes a worktree directory.
func RemoveWorktree(ctx context.Context, dir, path string) error {
	return RemoveWorktreeForce(ctx, dir, path, true)
}

// RemoveWorktreeForce removes a worktree, discarding changes when force.
func RemoveWorktreeForce(ctx context.Context, dir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := run(ctx, dir, args...)
	return err
}

// FileChange is one path's tracked line changes.
type FileChange struct {
	Path       string
	Insertions int
	Deletions  int
}

// WorkingTree lists the tracked changes from HEAD and the totals, plus
// whether the branch has an upstream.
func WorkingTree(ctx context.Context, dir string) (files []FileChange, insertions, deletions int, upstream bool) {
	if out, err := run(ctx, dir, "diff", "--numstat", "HEAD"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) < 3 {
				continue
			}
			ins, _ := strconv.Atoi(fields[0])
			del, _ := strconv.Atoi(fields[1])
			files = append(files, FileChange{Path: fields[2], Insertions: ins, Deletions: del})
			insertions += ins
			deletions += del
		}
	}
	if out, err := run(ctx, dir, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) != "" {
				files = append(files, FileChange{Path: line})
			}
		}
	}
	if _, err := run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		upstream = true
	}
	return files, insertions, deletions, upstream
}

// HasRemote reports whether the checkout has any remote.
func HasRemote(ctx context.Context, dir string) bool {
	out, err := run(ctx, dir, "remote")
	return err == nil && strings.TrimSpace(out) != ""
}

// CreateRef makes a branch, switching to it when switchRef is set.
func CreateRef(ctx context.Context, dir, name string, switchRef bool) error {
	if switchRef {
		_, err := run(ctx, dir, "checkout", "-b", name)
		return err
	}
	_, err := run(ctx, dir, "branch", name)
	return err
}

// SwitchRef checks out a branch or ref.
func SwitchRef(ctx context.Context, dir, name string) error {
	_, err := run(ctx, dir, "checkout", name)
	return err
}

// Pull fast-forwards the current branch and reports whether it moved.
func Pull(ctx context.Context, dir string) (status, upstreamRef string, err error) {
	if out, uerr := run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); uerr == nil {
		upstreamRef = strings.TrimSpace(out)
	}
	out, err := run(ctx, dir, "pull", "--ff-only")
	if err != nil {
		return "", upstreamRef, err
	}
	if strings.Contains(out, "Already up to date") {
		return "skipped_up_to_date", upstreamRef, nil
	}
	return "pulled", upstreamRef, nil
}

// ErrNoChanges says a commit found nothing to record.
var ErrNoChanges = errors.New("no changes to commit")

// Stage adds the given paths, or everything, to the index.
func Stage(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		_, err := run(ctx, dir, "add", "-A")
		return err
	}
	args := append([]string{"add", "--"}, paths...)
	_, err := run(ctx, dir, args...)
	return err
}

// Commit records the staged changes and returns the new commit's id and
// subject. A clean tree returns ErrNoChanges.
func Commit(ctx context.Context, dir, message string) (string, string, error) {
	if strings.TrimSpace(message) == "" {
		message = "Update from T3Go"
	}
	if _, err := run(ctx, dir, "commit", "-m", message); err != nil {
		if out, serr := run(ctx, dir, "status", "--porcelain"); serr == nil && strings.TrimSpace(out) == "" {
			return "", "", ErrNoChanges
		}
		return "", "", err
	}
	sha, _ := run(ctx, dir, "rev-parse", "HEAD")
	subject, _ := run(ctx, dir, "log", "-1", "--pretty=%s")
	return strings.TrimSpace(sha), strings.TrimSpace(subject), nil
}

// Push publishes the current branch, setting its upstream when it has none.
func Push(ctx context.Context, dir, branch string) error {
	if branch == "" {
		return errors.New("no branch to push")
	}
	if _, err := run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		_, err := run(ctx, dir, "push")
		return err
	}
	_, err := run(ctx, dir, "push", "-u", "origin", branch)
	return err
}

// ErrGHMissing says the GitHub CLI is not on PATH.
var ErrGHMissing = errors.New("GitHub CLI (gh) is not installed")

// PullRequest is the pull request for a branch as GitHub reports it.
type PullRequest struct {
	URL        string `json:"url"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	BaseBranch string `json:"baseBranch"`
	HeadBranch string `json:"headBranch"`
	State      string `json:"state"`
	// Existed is true when the branch already had one open.
	Existed bool `json:"existed,omitempty"`
}

// runGH runs the GitHub CLI in dir and returns its trimmed output.
func runGH(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr == "" {
			stderr = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", strings.Join(args, " "), stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// ExistingPullRequest reads the open pull request for the current branch.
func ExistingPullRequest(ctx context.Context, dir string) (PullRequest, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return PullRequest{}, ErrGHMissing
	}
	out, err := runGH(ctx, dir, "pr", "view", "--json", "url,number,title,baseRefName,headRefName,state")
	if err != nil {
		return PullRequest{}, err
	}
	var raw struct {
		URL         string `json:"url"`
		Number      int    `json:"number"`
		Title       string `json:"title"`
		BaseRefName string `json:"baseRefName"`
		HeadRefName string `json:"headRefName"`
		State       string `json:"state"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return PullRequest{}, fmt.Errorf("read pull request: %w", err)
	}
	if raw.URL == "" {
		return PullRequest{}, errors.New("pull request has no url")
	}
	return PullRequest{URL: raw.URL, Number: raw.Number, Title: raw.Title,
		BaseBranch: raw.BaseRefName, HeadBranch: raw.HeadRefName, State: raw.State}, nil
}

// CreatePullRequest opens a pull request for the current branch, or
// returns the one that is already open. base, when set, overrides the
// repository's default base branch.
func CreatePullRequest(ctx context.Context, dir, base string) (PullRequest, error) {
	if existing, err := ExistingPullRequest(ctx, dir); err == nil {
		existing.Existed = true
		return existing, nil
	} else if errors.Is(err, ErrGHMissing) {
		return PullRequest{}, err
	}
	args := []string{"pr", "create", "--fill"}
	if strings.TrimSpace(base) != "" {
		args = append(args, "--base", base)
	}
	out, err := runGH(ctx, dir, args...)
	if err != nil {
		return PullRequest{}, err
	}
	if pr, err := ExistingPullRequest(ctx, dir); err == nil {
		return pr, nil
	}
	// The create command prints the URL even when the follow-up view
	// fails on an older gh, so never lose a PR that was created.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") {
			return PullRequest{URL: line}, nil
		}
	}
	return PullRequest{}, errors.New("pull request was created but its URL could not be read")
}

// ListRefs lists local branches and, when asked, remote-tracking branches,
// newest state first: the checked-out branch, the default branch, then the
// rest by name.
func ListRefs(ctx context.Context, dir string, includeRemote bool) ([]proto.Branch, error) {
	if !IsRepo(ctx, dir) {
		return nil, ErrNotRepo
	}
	status := Status(ctx, dir)
	current := status.Branch
	def := DefaultBranch(ctx, dir)
	worktrees := worktreeBranches(ctx, dir)

	local, err := run(ctx, dir, "for-each-ref",
		"--format=%(refname:short)%09%(upstream)%09%(refname)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var refs []proto.Branch
	for _, line := range strings.Split(local, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		refs = append(refs, proto.Branch{
			Name:         fields[0],
			Current:      fields[0] == current,
			Default:      fields[0] == def,
			UpstreamRef:  fields[1],
			WorktreePath: worktrees[fields[2]],
		})
	}
	if includeRemote {
		remote, err := run(ctx, dir, "for-each-ref",
			"--format=%(refname:short)%09%(refname)", "refs/remotes")
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(remote, "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) < 2 || fields[0] == "" || strings.HasSuffix(fields[0], "/HEAD") {
				continue
			}
			remoteName := strings.SplitN(fields[0], "/", 2)[0]
			short := strings.TrimPrefix(fields[0], remoteName+"/")
			refs = append(refs, proto.Branch{
				Name:        fields[0],
				Default:     short == def,
				Remote:      true,
				RemoteName:  remoteName,
				UpstreamRef: fields[1],
			})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool {
		rank := func(b proto.Branch) int {
			switch {
			case b.Current:
				return 0
			case b.Default:
				return 1
			default:
				return 2
			}
		}
		if ri, rj := rank(refs[i]), rank(refs[j]); ri != rj {
			return ri < rj
		}
		return refs[i].Name < refs[j].Name
	})
	return refs, nil
}

// DefaultBranch is the branch a checkout compares against: origin's HEAD,
// then main, master, or trunk, then the current branch.
func DefaultBranch(ctx context.Context, dir string) string {
	if out, err := run(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if _, short, ok := strings.Cut(out, "/"); ok {
			return short
		}
	}
	branches, _ := Branches(ctx, dir)
	for _, name := range []string{"main", "master", "trunk"} {
		for _, branch := range branches {
			if branch == name {
				return name
			}
		}
	}
	return Status(ctx, dir).Branch
}

// worktreeBranches maps full branch refs to the worktrees that hold them.
func worktreeBranches(ctx context.Context, dir string) map[string]string {
	out, err := run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	paths := map[string]string{}
	var path string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			if path != "" {
				paths[strings.TrimPrefix(line, "branch ")] = path
			}
		}
	}
	return paths
}

// Init makes dir a Git repository when it is not one yet.
func Init(ctx context.Context, dir string) error {
	if IsRepo(ctx, dir) {
		return nil
	}
	_, err := run(ctx, dir, "init")
	return err
}

// GraphCommits lists commits from all refs, newest first, with parents
// and the labels pointing at them.
func GraphCommits(ctx context.Context, dir string, limit int) ([]proto.GraphCommit, error) {
	if !IsRepo(ctx, dir) {
		return nil, ErrNotRepo
	}
	if limit <= 0 {
		limit = 120
	}
	out, err := run(ctx, dir, "log", "--branches", "--remotes", "--tags", "HEAD", "--date-order",
		fmt.Sprintf("--max-count=%d", limit), "--format=%H%x1f%P%x1f%an%x1f%cI%x1f%s")
	if err != nil {
		return nil, err
	}
	head, _ := run(ctx, dir, "rev-parse", "HEAD")
	current := Status(ctx, dir).Branch
	refs := graphRefs(ctx, dir, current)
	var commits []proto.GraphCommit
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x1f")
		if len(fields) < 5 {
			continue
		}
		commit := proto.GraphCommit{Hash: fields[0], Subject: fields[4], Author: fields[2], Head: fields[0] == head}
		commit.Short = commit.Hash
		if len(commit.Short) > 7 {
			commit.Short = commit.Short[:7]
		}
		commit.Parents = strings.Fields(fields[1])
		if date, err := time.Parse(time.RFC3339, fields[3]); err == nil {
			commit.Date = date
		}
		commit.Refs = refs[commit.Hash]
		commits = append(commits, commit)
	}
	return commits, nil
}

// graphRefs maps commit hashes to the refs that point at them.
func graphRefs(ctx context.Context, dir, current string) map[string][]proto.GraphRef {
	out, err := run(ctx, dir, "for-each-ref",
		"--format=%(objectname)%09%(*objectname)%09%(refname:short)%09%(refname)", "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil
	}
	refs := map[string][]proto.GraphRef{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 4 || strings.HasSuffix(fields[3], "/HEAD") {
			continue
		}
		hash := fields[0]
		if fields[1] != "" {
			hash = fields[1]
		}
		kind := "branch"
		switch {
		case strings.HasPrefix(fields[3], "refs/remotes/"):
			kind = "remote"
		case strings.HasPrefix(fields[3], "refs/tags/"):
			kind = "tag"
		}
		refs[hash] = append(refs[hash], proto.GraphRef{Name: fields[2], Kind: kind,
			Current: kind == "branch" && fields[2] == current})
	}
	return refs
}
