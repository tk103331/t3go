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

// handleAgentCommands lists workspace and user skills/custom commands.
func (s *Server) handleAgentCommands(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	root := s.Service.ThreadDir(r.Context(), t)
	commands := discoverCommands(root)
	commands = append(commands, discoverMCPPrompts(root)...)
	writeJSON(w, 200, map[string]any{"commands": commands})
}

type commandSource struct {
	dir    string
	source string
	kind   string
}

func discoverCommands(root string) []proto.AgentCommand {
	home, _ := os.UserHomeDir()
	var sources []commandSource
	for _, base := range []string{root, home} {
		if base == "" {
			continue
		}
		sources = append(sources,
			commandSource{filepath.Join(base, ".claude", "commands"), "claude", "command"},
			commandSource{filepath.Join(base, ".claude", "skills"), "claude", "skill"},
			commandSource{filepath.Join(base, ".opencode", "command"), "opencode", "command"},
			commandSource{filepath.Join(base, ".opencode", "skills"), "opencode", "skill"},
			commandSource{filepath.Join(base, ".codex", "prompts"), "codex", "command"},
		)
	}
	seen := map[string]bool{}
	var out []proto.AgentCommand
	for _, source := range sources {
		if source.kind == "skill" {
			out = append(out, discoverSkills(source, seen)...)
			continue
		}
		out = append(out, discoverFiles(source, seen)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func discoverFiles(source commandSource, seen map[string]bool) []proto.AgentCommand {
	var out []proto.AgentCommand
	_ = filepath.WalkDir(source.dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(source.dir, path)
		if relErr != nil {
			return nil
		}
		name, description := commandFrontmatter(path)
		if name == "" {
			name = strings.TrimSuffix(rel, filepath.Ext(rel))
			name = strings.ReplaceAll(filepath.ToSlash(name), "/", ":")
		}
		key := source.source + ":" + name
		if seen[key] {
			return nil
		}
		seen[key] = true
		out = append(out, proto.AgentCommand{Name: name, Description: description, Kind: source.kind,
			Source: source.source, Path: filepath.ToSlash(rel)})
		return nil
	})
	return out
}

func discoverSkills(source commandSource, seen map[string]bool) []proto.AgentCommand {
	var out []proto.AgentCommand
	entries, err := os.ReadDir(source.dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(source.dir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		name, description := commandFrontmatter(path)
		if name == "" {
			name = entry.Name()
		}
		key := source.source + ":" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, proto.AgentCommand{Name: name, Description: description, Kind: source.kind,
			Source: source.source, Path: filepath.ToSlash(filepath.Join(entry.Name(), "SKILL.md"))})
	}
	return out
}

// commandFrontmatter reads a command's name and description from a small
// YAML frontmatter block, if it has one.
func commandFrontmatter(path string) (name, description string) {
	file, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer file.Close()
	buf := make([]byte, 4096)
	n, _ := file.Read(buf)
	text := string(buf[:n])
	if !strings.HasPrefix(strings.TrimSpace(text), "---") {
		return "", ""
	}
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			name = strings.ReplaceAll(value, " ", "-")
		case "description":
			description = value
		}
	}
	return name, description
}
