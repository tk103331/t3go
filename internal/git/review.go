package git

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BranchCommit is one commit on a branch since its base.
type BranchCommit struct {
	SHA         string
	Subject     string
	CommittedAt string
}

// DiffFile is one changed path in a diff.
type DiffFile struct {
	Path         string
	PreviousPath string
	Additions    int
	Deletions    int
}

// DiffRange is a patch and its file stats.
type DiffRange struct {
	Patch      string
	Files      []DiffFile
	Truncated  bool
	Incomplete bool
}

// EmptyTree is Git's hash for an empty tree, the base of a root commit.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// ReviewBase resolves baseRef, or the default branch, to a commit that
// HEAD can be compared with.
func ReviewBase(ctx context.Context, dir, baseRef string) string {
	base := strings.TrimSpace(baseRef)
	if base == "" {
		base = DefaultBranch(ctx, dir)
	}
	if base == "" {
		return ""
	}
	if out, err := run(ctx, dir, "merge-base", base, "HEAD"); err == nil && out != "" {
		return out
	}
	return base
}

// BranchCommits lists commits from base to HEAD, newest first.
func BranchCommits(ctx context.Context, dir, baseRef string, limit int) ([]BranchCommit, bool, error) {
	if !IsRepo(ctx, dir) {
		return nil, false, ErrNotRepo
	}
	if limit <= 0 {
		limit = 100
	}
	base := ReviewBase(ctx, dir, baseRef)
	args := []string{"log", "--no-notes", "--no-show-signature",
		"--format=%H%x1f%s%x1f%cI", fmt.Sprintf("--max-count=%d", limit+1)}
	if base != "" {
		args = append(args, base+"..HEAD")
	}
	out, err := run(ctx, dir, args...)
	if err != nil {
		return nil, false, err
	}
	var commits []BranchCommit
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x1f")
		if len(fields) < 3 {
			continue
		}
		commits = append(commits, BranchCommit{SHA: fields[0], Subject: fields[1], CommittedAt: fields[2]})
	}
	truncated := len(commits) > limit
	if truncated {
		commits = commits[:limit]
	}
	return commits, truncated, nil
}

// CommitMeta reads a commit's subject and first parent.
func CommitMeta(ctx context.Context, dir, sha string) (subject, parent string, err error) {
	out, err := run(ctx, dir, "show", "-s", "--format=%P%x1f%s", sha)
	if err != nil {
		return "", "", err
	}
	fields := strings.SplitN(out, "\x1f", 2)
	if len(fields) == 2 {
		if parents := strings.Fields(fields[0]); len(parents) > 0 {
			parent = parents[0]
		}
		return fields[1], parent, nil
	}
	return "", "", errors.New("could not read commit")
}

// ReviewDiff computes the patch and file stats between two refs. An empty
// to means the working tree; from is then the tree to compare it with.
func ReviewDiff(ctx context.Context, dir, from, to string, paths []string, ignoreWhitespace, trackedOnly, withPatch bool) (DiffRange, error) {
	if !IsRepo(ctx, dir) {
		return DiffRange{}, ErrNotRepo
	}
	base := []string{"diff", "--no-renames", "--minimal"}
	if ignoreWhitespace {
		base = append(base, "--ignore-all-space")
	}
	refs := []string{}
	if from != "" {
		refs = append(refs, from)
	}
	if to != "" {
		refs = append(refs, to)
	}
	pathArgs := []string{}
	if len(paths) > 0 {
		pathArgs = append(pathArgs, "--")
		pathArgs = append(pathArgs, paths...)
	}

	statsArgs := append(append(append([]string{}, base...), "--numstat"), refs...)
	statsArgs = append(statsArgs, pathArgs...)
	out, err := run(ctx, dir, statsArgs...)
	if err != nil {
		return DiffRange{}, err
	}
	result := DiffRange{Files: parseNumstat(out)}
	if to == "" && !trackedOnly {
		untracked, lines, err := untrackedFiles(ctx, dir, paths)
		if err != nil {
			return DiffRange{}, err
		}
		for i, path := range untracked {
			result.Files = append(result.Files, DiffFile{Path: path, Additions: lines[i]})
		}
	}
	if !withPatch {
		return result, nil
	}

	patchArgs := append(append(append([]string{}, base...), "--patch"), refs...)
	patchArgs = append(patchArgs, pathArgs...)
	patch, err := run(ctx, dir, patchArgs...)
	if err != nil {
		return DiffRange{}, err
	}
	if to == "" && !trackedOnly {
		untracked, _, err := untrackedFiles(ctx, dir, paths)
		if err != nil {
			return DiffRange{}, err
		}
		if len(untracked) > 0 {
			var extra strings.Builder
			for _, path := range untracked {
				out, err := runAllowExitOne(ctx, dir, "diff", "--no-index", "--", os.DevNull, path)
				if err != nil {
					return DiffRange{}, err
				}
				extra.WriteString(out)
				if out != "" && !strings.HasSuffix(out, "\n") {
					extra.WriteString("\n")
				}
			}
			patch = strings.TrimRight(patch, "\n")
			if patch != "" && extra.Len() > 0 {
				patch += "\n"
			}
			patch += extra.String()
		}
	}
	const maxPatch = 400_000
	if len(patch) > maxPatch {
		patch = patch[:maxPatch]
		result.Truncated = true
	}
	result.Patch = patch
	return result, nil
}

// FileContents reads the old and new sides of one changed file.
func FileContents(ctx context.Context, dir, sourceKind, baseRef, headRef, oldPath, newPath string) (string, string, error) {
	root, err := Root(ctx, dir)
	if err != nil {
		return "", "", err
	}
	var oldRef, newRef string
	switch sourceKind {
	case "commit":
		oldRef, newRef = baseRef, headRef
	case "branch-range":
		oldRef = baseRef
		newRef = headRef
		if newRef == "" {
			newRef = "HEAD"
		}
	default: // working tree
		oldRef = baseRef
		if oldRef == "" {
			oldRef = "HEAD"
		}
	}
	oldContents := ""
	if oldRef != "" {
		oldContents, _ = showBlob(ctx, dir, oldRef, oldPath)
	}
	var newContents string
	if sourceKind == "working-tree" {
		full, err := withinRoot(ctx, root, newPath)
		if err != nil {
			return "", "", err
		}
		if raw, err := os.ReadFile(full); err == nil {
			newContents = string(raw)
		}
	} else if newRef != "" {
		newContents, _ = showBlob(ctx, dir, newRef, newPath)
	}
	return oldContents, newContents, nil
}

// DiffHash is a stable digest of a patch and its file stats.
func DiffHash(patch string, files []DiffFile) string {
	sum := sha256.New()
	sum.Write([]byte(patch))
	for _, file := range files {
		fmt.Fprintf(sum, "\x00%s\x00%s\x00%d\x00%d", file.Path, file.PreviousPath, file.Additions, file.Deletions)
	}
	return fmt.Sprintf("%x", sum.Sum(nil))
}

func parseNumstat(out string) []DiffFile {
	var files []DiffFile
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		additions, _ := strconv.Atoi(fields[0])
		deletions, _ := strconv.Atoi(fields[1])
		files = append(files, DiffFile{Path: fields[2], Additions: additions, Deletions: deletions})
	}
	return files
}

// untrackedFiles lists untracked files and their line counts.
func untrackedFiles(ctx context.Context, dir string, paths []string) ([]string, []int, error) {
	args := []string{"ls-files", "--others", "--exclude-standard"}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, err := run(ctx, dir, args...)
	if err != nil {
		return nil, nil, err
	}
	var names []string
	var lines []int
	for _, path := range strings.Split(out, "\n") {
		if strings.TrimSpace(path) == "" {
			continue
		}
		names = append(names, path)
		count := 0
		if raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path))); err == nil {
			count = strings.Count(string(raw), "\n")
			if len(raw) > 0 && raw[len(raw)-1] != '\n' {
				count++
			}
		}
		lines = append(lines, count)
	}
	return names, lines, nil
}

func withinRoot(ctx context.Context, root, path string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(path))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("path is outside the repository")
	}
	return full, nil
}

// showBlob reads a path at a ref without trimming the file's bytes.
func showBlob(ctx context.Context, dir, ref, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "show", ref+":"+path)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// runAllowExitOne runs Git and treats exit code 1 as success, which is how
// diff --no-index reports a difference.
func runAllowExitOne(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimRight(string(out), "\n"), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return strings.TrimRight(string(out), "\n"), nil
	}
	return "", err
}
