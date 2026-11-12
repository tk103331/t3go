package server

import (
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
	home := hostHome()
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	switch {
	case path == "" || path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(home, path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "dirs", err.Error())
		return
	}
	if !info.IsDir() {
		writeError(w, http.StatusBadRequest, "dirs", path+" is not a folder")
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "dirs", err.Error())
		return
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
	writeJSON(w, http.StatusOK, proto.HostDirs{Path: path, Parent: parent, Home: home, Entries: items})
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
