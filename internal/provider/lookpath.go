package provider

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func lookPath(name string) (string, error) {
	for _, dir := range providerPathDirs() {
		if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

// providerCommandContext keeps provider CLIs working when the environment
// server was launched by a desktop service with a minimal PATH. Node-based
// CLIs re-resolve "node" through /usr/bin/env, so finding only the absolute
// CLI path is not enough: their child environment needs the same augmented
// PATH.
func providerCommandContext(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = providerEnvironment()
	return cmd
}

func providerEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "PATH=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "PATH="+strings.Join(providerPathDirs(), string(os.PathListSeparator)))
	return env
}

// providerPathDirs includes the process PATH first, then the common places
// npm, nvm, Volta, Bun, Homebrew and OpenCode install CLIs. This covers a
// login shell's tools without requiring the server to import shell startup
// files.
func providerPathDirs() []string {
	var dirs []string
	dirs = append(dirs, filepath.SplitList(os.Getenv("PATH"))...)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".asdf", "shims"),
			filepath.Join(home, ".local", "share", "mise", "shims"),
			filepath.Join(home, ".local", "share", "fnm", "aliases", "default", "bin"),
			filepath.Join(home, "Library", "pnpm"),
		)
		matches, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin"))
		sort.Slice(matches, func(i, j int) bool {
			return newestPath(matches[i], matches[j])
		})
		dirs = append(dirs, matches...)
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	if runtime.GOOS == "windows" {
		dirs = append(dirs,
			filepath.Join(os.Getenv("APPDATA"), "npm"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"),
		)
	}
	return uniqueDirs(dirs)
}

func newestPath(a, b string) bool {
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	if aerr != nil || berr != nil {
		return a > b
	}
	return ai.ModTime().After(bi.ModTime())
}

func uniqueDirs(dirs []string) []string {
	seen := make(map[string]bool, len(dirs))
	unique := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		unique = append(unique, dir)
	}
	return unique
}
