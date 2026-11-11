package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/proto"
)

// handleWorkingDiff returns the checkout's changes against HEAD.
func (s *Server) handleWorkingDiff(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	dir := s.Service.ThreadDir(r.Context(), t)
	diff, err := git.ReviewDiff(r.Context(), dir, "HEAD", "", nil, false, false, true)
	if err != nil {
		writeError(w, statusFor(err), "diff", err.Error())
		return
	}
	staged := git.StagedPaths(r.Context(), dir)
	untracked := git.UntrackedPaths(r.Context(), dir)
	files := make([]proto.DiffFile, 0, len(diff.Files))
	for _, file := range diff.Files {
		files = append(files, proto.DiffFile{Path: file.Path, Additions: file.Additions, Deletions: file.Deletions,
			Staged: staged[file.Path], Untracked: untracked[file.Path]})
	}
	writeJSON(w, 200, proto.WorkingDiff{Patch: diff.Patch, Files: files})
}

func (s *Server) handleStageFiles(w http.ResponseWriter, r *http.Request) {
	s.gitPathsAction(w, r, true)
}

func (s *Server) handleDiscardFiles(w http.ResponseWriter, r *http.Request) {
	s.gitPathsAction(w, r, false)
}

func (s *Server) gitPathsAction(w http.ResponseWriter, r *http.Request, stage bool) {
	var input struct {
		Paths []string `json:"paths"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	dir := s.Service.ThreadDir(r.Context(), t)
	paths := make([]string, 0, len(input.Paths))
	for _, path := range input.Paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if _, err := workspacePath(dir, path); err != nil {
			writeError(w, 400, "git", err.Error())
			return
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		writeError(w, 400, "git", "paths are required")
		return
	}
	if stage {
		err = git.Stage(r.Context(), dir, paths)
	} else {
		err = git.DiscardPaths(r.Context(), dir, paths)
	}
	if err != nil {
		writeError(w, statusFor(err), "git", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleGitPull fast-forwards the checkout from its upstream.
func (s *Server) handleGitPull(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	dir := s.Service.ThreadDir(r.Context(), t)
	status, ref, err := git.Pull(r.Context(), dir)
	if err != nil {
		writeError(w, statusFor(err), "git", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": status, "refName": ref})
}

// handlePullRequests lists the branch's pull requests.
func (s *Server) handlePullRequests(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	pulls, err := git.ListPullRequests(r.Context(), s.Service.ThreadDir(r.Context(), t))
	if err != nil {
		writeJSON(w, 200, map[string]any{"pullRequests": []any{}, "detail": err.Error()})
		return
	}
	items := make([]proto.PullRequest, 0, len(pulls))
	for _, pr := range pulls {
		items = append(items, proto.PullRequest{URL: pr.URL, Number: pr.Number, Title: pr.Title,
			BaseBranch: pr.BaseBranch, HeadBranch: pr.HeadBranch, State: pr.State, Draft: pr.Draft})
	}
	writeJSON(w, 200, map[string]any{"pullRequests": items})
}

// handlePullRequestDetail reads one pull request's checks and reviews.
func (s *Server) handlePullRequestDetail(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		writeError(w, 400, "pull request", "invalid number")
		return
	}
	detail, err := git.PullRequestDetails(r.Context(), s.Service.ThreadDir(r.Context(), t), number)
	if err != nil {
		writeError(w, statusFor(err), "pull request", err.Error())
		return
	}
	writeJSON(w, 200, detail)
}

// handlePullRequestMerge merges a pull request.
func (s *Server) handlePullRequestMerge(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Method       string `json:"method"`
		DeleteBranch bool   `json:"deleteBranch"`
	}
	_ = decodeJSON(r, &input)
	s.pullRequestAction(w, r, "merge", input.Method, input.DeleteBranch)
}

// handlePullRequestAction readies, drafts, closes or reopens a pull request.
func (s *Server) handlePullRequestAction(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action string `json:"action"`
	}
	_ = decodeJSON(r, &input)
	s.pullRequestAction(w, r, input.Action, "", false)
}

func (s *Server) pullRequestAction(w http.ResponseWriter, r *http.Request, action, method string, deleteBranch bool) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		writeError(w, 400, "pull request", "invalid number")
		return
	}
	if err := git.PullRequestAction(r.Context(), s.Service.ThreadDir(r.Context(), t), number, action, method, deleteBranch); err != nil {
		writeError(w, statusFor(err), "pull request", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
