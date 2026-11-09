// Package git reads repository state and captures checkpoints for T3Go.
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	_, err := run(ctx, dir, "worktree", "remove", "--force", path)
	return err
}
