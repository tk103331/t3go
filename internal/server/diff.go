package server

import (
	"net/http"
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
