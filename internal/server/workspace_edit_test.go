package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// The checkout's file tree can add, rename and remove entries, while
// refusing names and paths that leave the checkout.
func TestWorkspaceFileEditing(t *testing.T) {
	e := newTestEnv(t)
	project := e.project(t)
	thread := e.thread(t, project.ID)
	base := "/api/v1/threads/" + thread.ID

	var created struct {
		Path      string `json:"path"`
		Directory bool   `json:"directory"`
	}
	if code := e.request(t, http.MethodPost, base+"/files",
		map[string]any{"path": "", "name": "note.txt"}, e.token, &created); code != http.StatusCreated {
		t.Fatalf("create file = %d", code)
	}
	if created.Path != "note.txt" {
		t.Fatalf("create file path = %q", created.Path)
	}
	if _, err := os.Stat(filepath.Join(project.Path, "note.txt")); err != nil {
		t.Fatalf("the file is not on disk: %v", err)
	}
	if code := e.request(t, http.MethodPost, base+"/files",
		map[string]any{"path": "", "name": "note.txt"}, e.token, nil); code != http.StatusConflict {
		t.Fatalf("duplicate file = %d", code)
	}
	for _, name := range []string{"../escape", "a/b", "..", ".", ".git"} {
		if code := e.request(t, http.MethodPost, base+"/files",
			map[string]any{"path": "", "name": name}, e.token, nil); code != http.StatusBadRequest {
			t.Fatalf("name %q = %d", name, code)
		}
	}
	if code := e.request(t, http.MethodPost, base+"/files",
		map[string]any{"path": "", "name": "folder", "directory": true}, e.token, nil); code != http.StatusCreated {
		t.Fatalf("create folder = %d", code)
	}
	if info, err := os.Stat(filepath.Join(project.Path, "folder")); err != nil || !info.IsDir() {
		t.Fatalf("the folder is not on disk: %v", err)
	}
	if code := e.request(t, http.MethodPost, base+"/files/rename",
		map[string]string{"from": "note.txt", "to": "folder/renamed.txt"}, e.token, nil); code != http.StatusOK {
		t.Fatalf("rename = %d", code)
	}
	if _, err := os.Stat(filepath.Join(project.Path, "folder", "renamed.txt")); err != nil {
		t.Fatalf("the renamed file is not on disk: %v", err)
	}
	if code := e.request(t, http.MethodPost, base+"/files/rename",
		map[string]string{"from": "folder/renamed.txt", "to": "../outside.txt"}, e.token, nil); code != http.StatusBadRequest {
		t.Fatalf("escaping rename = %d", code)
	}
	if code := e.request(t, http.MethodDelete, base+"/files?path="+url.QueryEscape("../outside.txt"),
		nil, e.token, nil); code != http.StatusBadRequest {
		t.Fatalf("escaping delete = %d", code)
	}
	if code := e.request(t, http.MethodDelete, base+"/files?path="+url.QueryEscape("folder"),
		nil, e.token, nil); code != http.StatusOK {
		t.Fatalf("delete folder = %d", code)
	}
	if _, err := os.Stat(filepath.Join(project.Path, "folder")); !os.IsNotExist(err) {
		t.Fatalf("the folder is still on disk: %v", err)
	}
	if code := e.request(t, http.MethodDelete, base+"/files?path=", nil, e.token, nil); code != http.StatusBadRequest {
		t.Fatalf("empty delete = %d", code)
	}
	if code := e.request(t, http.MethodDelete, base+"/files?path="+url.QueryEscape(".git"), nil, e.token, nil); code != http.StatusBadRequest {
		t.Fatalf("delete .git = %d", code)
	}
}
