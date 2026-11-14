package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tk/t3go/internal/proto"
)

// handleHostDirs lists the folders in one directory of the machine
// running the environment, so a client can pick a project folder it
// cannot see itself.
func (s *Server) handleHostDirs(w http.ResponseWriter, r *http.Request) {
	path := resolveHostDir(r.URL.Query().Get("path"))
	listing, err := listHostDirs(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "dirs", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

// handleCreateHostDir creates one folder below the requested host directory.
func (s *Server) handleCreateHostDir(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "dirs", "invalid request")
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) ||
		filepath.Base(name) != name {
		writeError(w, http.StatusBadRequest, "dirs", "folder name must be a single path component")
		return
	}
	parent := resolveHostDir(input.Path)
	info, err := os.Stat(parent)
	if err != nil {
		writeError(w, http.StatusBadRequest, "dirs", err.Error())
		return
	}
	if !info.IsDir() {
		writeError(w, http.StatusBadRequest, "dirs", parent+" is not a folder")
		return
	}
	if err := os.Mkdir(filepath.Join(parent, name), 0o755); err != nil {
		writeError(w, http.StatusBadRequest, "dirs", err.Error())
		return
	}
	listing, err := listHostDirs(parent)
	if err != nil {
		writeError(w, http.StatusBadRequest, "dirs", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, listing)
}

func resolveHostDir(raw string) string {
	home := hostHome()
	path := strings.TrimSpace(raw)
	switch {
	case path == "" || path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(home, path)
	}
	return filepath.Clean(path)
}

func listHostDirs(path string) (proto.HostDirs, error) {
	home := hostHome()
	info, err := os.Stat(path)
	if err != nil {
		return proto.HostDirs{}, err
	}
	if !info.IsDir() {
		return proto.HostDirs{}, fmt.Errorf("%s is not a folder", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return proto.HostDirs{}, err
	}
	const limit = 1000
	items := make([]proto.HostDir, 0, len(entries))
	for _, entry := range entries {
		isDir := entry.IsDir()
		if !isDir && entry.Type()&fs.ModeSymlink != 0 {
			if target, err := os.Stat(filepath.Join(path, entry.Name())); err == nil {
				isDir = target.IsDir()
			}
		}
		if !isDir {
			continue
		}
		items = append(items, proto.HostDir{Name: entry.Name(), Path: filepath.Join(path, entry.Name())})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	return proto.HostDirs{Path: path, Parent: parent, Home: home, Entries: items}, nil
}

// hostHome is the environment user's home folder, or its working folder
// when the platform does not report one.
func hostHome() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		return cwd
	}
	return string(os.PathSeparator)
}
