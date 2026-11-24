package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// workspaceName validates one file or folder name, which must not name
// its own parent or another directory.
func workspaceName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", errors.New("invalid name")
	}
	return name, nil
}

// gitPathRefused keeps the checkout's .git directory out of reach: its
// contents are not the repository's files to browse or change.
func gitPathRefused(rel string) error {
	rel = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(rel)), "/")
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return errors.New("the .git directory is read-only here")
	}
	return nil
}

// threadCheckout resolves a thread's checkout plus a slash path inside it.
func (s *Server) threadCheckout(w http.ResponseWriter, r *http.Request, path string) (string, string, bool) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return "", "", false
	}
	root := s.Service.ThreadDir(r.Context(), t)
	full, err := workspacePath(root, path)
	if err != nil {
		writeError(w, 400, "files", err.Error())
		return "", "", false
	}
	return root, full, true
}

// handleCreateWorkspaceEntry adds one file or folder to a checkout.
func (s *Server) handleCreateWorkspaceEntry(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path      string `json:"path"`
		Name      string `json:"name"`
		Directory bool   `json:"directory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	name, err := workspaceName(in.Name)
	if err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	if err := gitPathRefused(pathJoin(in.Path, name)); err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	root, dir, ok := s.threadCheckout(w, r, in.Path)
	if !ok {
		return
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		writeError(w, 400, "files", "that path is not a folder")
		return
	}
	target := filepath.Join(dir, name)
	if _, err := os.Lstat(target); err == nil {
		writeError(w, 409, "files", "a file or folder with that name already exists")
		return
	}
	if in.Directory {
		err = os.Mkdir(target, 0o755)
	} else {
		var file *os.File
		file, err = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			err = file.Close()
		}
	}
	if err != nil {
		writeError(w, statusFor(err), "files", err.Error())
		return
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(target, root), string(os.PathSeparator))
	writeJSON(w, 201, map[string]any{"path": filepath.ToSlash(rel), "directory": in.Directory})
}

// handleRenameWorkspaceEntry moves one entry inside a checkout.
func (s *Server) handleRenameWorkspaceEntry(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	if err := gitPathRefused(in.From); err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	if err := gitPathRefused(in.To); err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	if strings.TrimSpace(in.From) == "" || strings.TrimSpace(in.To) == "" {
		writeError(w, 400, "files", "from and to are required")
		return
	}
	root, from, ok := s.threadCheckout(w, r, in.From)
	if !ok {
		return
	}
	if from == root {
		writeError(w, 400, "files", "cannot rename the checkout root")
		return
	}
	to, err := workspacePath(root, in.To)
	if err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	if to == root {
		writeError(w, 400, "files", "cannot rename onto the checkout root")
		return
	}
	if _, err := os.Lstat(to); err == nil {
		writeError(w, 409, "files", "the new name already exists")
		return
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		writeError(w, statusFor(err), "files", err.Error())
		return
	}
	if err := os.Rename(from, to); err != nil {
		writeError(w, statusFor(err), "files", err.Error())
		return
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(to, root), string(os.PathSeparator))
	writeJSON(w, 200, map[string]any{"path": filepath.ToSlash(rel)})
}

// handleDeleteWorkspaceEntry removes one file or folder from a checkout.
func (s *Server) handleDeleteWorkspaceEntry(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, 400, "files", "path is required")
		return
	}
	root, full, ok := s.threadCheckout(w, r, path)
	if !ok {
		return
	}
	if full == root {
		writeError(w, 400, "files", "cannot delete the checkout root")
		return
	}
	if err := gitPathRefused(path); err != nil {
		writeError(w, 400, "files", err.Error())
		return
	}
	if err := os.RemoveAll(full); err != nil {
		writeError(w, statusFor(err), "files", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"path": filepath.ToSlash(path)})
}
