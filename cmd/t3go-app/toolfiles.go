package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// toolChangedFiles is the set of files a file-editing tool names in its
// input. Providers describe edits differently, so this reads the usual
// path fields from their JSON and falls back to a bare path.
func toolChangedFiles(title, detail string) []string {
	if !toolEditsFiles(title) || strings.TrimSpace(detail) == "" {
		return nil
	}
	var seen = map[string]bool{}
	var paths []string
	add := func(path string) {
		path = strings.TrimSpace(strings.Trim(path, `"`))
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	var value any
	if json.Unmarshal([]byte(detail), &value) == nil {
		walkFilePaths(value, add)
	}
	if len(paths) == 0 && looksLikePath(detail) {
		add(detail)
	}
	return paths
}

func toolEditsFiles(title string) bool {
	name := strings.ToLower(title)
	for _, part := range []string{"edit", "write", "patch", "create", "delete", "move", "rename", "notebook", "replace"} {
		if strings.Contains(name, part) {
			return true
		}
	}
	return false
}

func walkFilePaths(value any, add func(string)) {
	switch item := value.(type) {
	case map[string]any:
		for _, key := range []string{"file_path", "filePath", "path", "filename", "fileName", "newPath", "oldPath"} {
			if path, ok := item[key].(string); ok && strings.TrimSpace(path) != "" {
				add(path)
			}
		}
		for _, child := range item {
			walkFilePaths(child, add)
		}
	case []any:
		for _, child := range item {
			walkFilePaths(child, add)
		}
	}
}

func looksLikePath(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\n\t ") || strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		return false
	}
	return strings.Contains(text, "/") || strings.Contains(text, ".")
}

// toolFileRelative keeps a path inside the checkout, for opening it.
func toolFileRelative(root, path string) string {
	path = strings.TrimSpace(path)
	if root == "" || path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ""
		}
		return filepath.ToSlash(rel)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}
