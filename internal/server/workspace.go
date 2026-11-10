package server

import (
	"bytes"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/proto"
)

// handleThreadFiles lists a directory in a thread's checkout.
func (s *Server) handleThreadFiles(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	root := s.Service.ThreadDir(r.Context(), t)
	rel := filepath.ToSlash(strings.TrimSpace(r.URL.Query().Get("path")))
	full, err := workspacePath(root, rel)
	if err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	items := make([]proto.WorkspaceEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		item := proto.WorkspaceEntry{Name: entry.Name(), Path: pathJoin(rel, entry.Name()), Directory: entry.IsDir()}
		if !entry.IsDir() {
			item.Size = info.Size()
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Directory != items[j].Directory {
			return items[i].Directory
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	writeJSON(w, 200, map[string]any{"path": rel, "entries": items})
}

// handleThreadFile reads one text file from a thread's checkout.
func (s *Server) handleThreadFile(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	root := s.Service.ThreadDir(r.Context(), t)
	rel := filepath.ToSlash(strings.TrimSpace(r.URL.Query().Get("path")))
	if rel == "" {
		writeError(w, 400, "file", "path is required")
		return
	}
	full, err := workspacePath(root, rel)
	if err != nil {
		writeError(w, 400, "file", err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		writeError(w, 400, "file", err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, 400, "file", "that path is a directory")
		return
	}
	const maxFile = 2 << 20
	if info.Size() > maxFile {
		writeError(w, 413, "file", "this file is too large to preview")
		return
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		writeError(w, 400, "file", err.Error())
		return
	}
	file := proto.WorkspaceFile{Path: rel, Size: int64(len(raw)), Mime: fileMime(rel, raw)}
	if bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
		file.Binary = true
	} else {
		file.Content = string(raw)
	}
	writeJSON(w, 200, file)
}

// handleThreadRaw serves a file's bytes, for image previews and opening
// other files outside the app.
func (s *Server) handleThreadRaw(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	root := s.Service.ThreadDir(r.Context(), t)
	rel := filepath.ToSlash(strings.TrimSpace(r.URL.Query().Get("path")))
	if rel == "" {
		writeError(w, 400, "file", "path is required")
		return
	}
	full, err := workspacePath(root, rel)
	if err != nil {
		writeError(w, 400, "file", err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		writeError(w, 400, "file", "not a file")
		return
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		writeError(w, 400, "file", err.Error())
		return
	}
	w.Header().Set("Content-Type", fileMime(rel, raw))
	w.Header().Set("Content-Disposition", "inline; filename=\""+strings.ReplaceAll(filepath.Base(rel), "\"", "")+"\"")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}

// fileMime infers a file's media type from its extension and bytes.
func fileMime(path string, raw []byte) string {
	if typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); typ != "" {
		return typ
	}
	if bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
		return "application/octet-stream"
	}
	return "text/plain; charset=utf-8"
}

// handleThreadGraph lists commits from all refs for the graph view.
func (s *Server) handleThreadGraph(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	limit := 120
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	commits, err := git.GraphCommits(r.Context(), s.Service.ThreadDir(r.Context(), t), limit)
	if err != nil {
		writeError(w, statusFor(err), "graph", err.Error())
		return
	}
	if commits == nil {
		commits = []proto.GraphCommit{}
	}
	writeJSON(w, 200, map[string]any{"commits": commits})
}

// handlePullRequest reads the open pull request for a thread's branch.
func (s *Server) handlePullRequest(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	pr, err := git.ExistingPullRequest(r.Context(), s.Service.ThreadDir(r.Context(), t))
	if err != nil {
		writeJSON(w, 200, map[string]any{"pullRequest": nil, "detail": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"pullRequest": proto.PullRequest{
		URL: pr.URL, Number: pr.Number, Title: pr.Title,
		BaseBranch: pr.BaseBranch, HeadBranch: pr.HeadBranch, State: pr.State,
	}})
}

// workspacePath resolves a slash path inside root, refusing escapes.
func workspacePath(root, rel string) (string, error) {
	if strings.ContainsRune(rel, 0) {
		return "", errors.New("invalid path")
	}
	clean := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(rel, "/")))
	if clean == "." || clean == "" {
		return root, nil
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path is outside the checkout")
	}
	full := filepath.Join(root, clean)
	checkRoot, rootErr := filepath.EvalSymlinks(root)
	checkFull, fullErr := filepath.EvalSymlinks(full)
	if rootErr == nil && fullErr == nil {
		inside, err := filepath.Rel(checkRoot, checkFull)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
			return "", errors.New("path is outside the checkout")
		}
	}
	return full, nil
}

func pathJoin(base, name string) string {
	if base == "" {
		return name
	}
	return strings.TrimSuffix(base, "/") + "/" + name
}
