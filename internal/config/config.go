// Package config resolves T3Go's data directory, token and listen address.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultPort is the server's port when none is given.
const DefaultPort = 4870

// Config is one server environment.
type Config struct {
	Home    string
	Listen  string
	Token   string
	NoAuth  bool
	Version string
	// WebDir, when set, holds the built web client to serve at /.
	WebDir string
}

// HomeDir returns T3GO_HOME, or ~/.t3go.
func HomeDir() string {
	if dir := strings.TrimSpace(os.Getenv("T3GO_HOME")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".t3go"
	}
	return filepath.Join(home, ".t3go")
}

// Load resolves the environment's paths and token.
func Load(home, listen string, noAuth bool) (Config, error) {
	if strings.TrimSpace(home) == "" {
		home = HomeDir()
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return Config{}, err
	}
	home = abs
	for _, dir := range []string{home, filepath.Join(home, "worktrees"), filepath.Join(home, "logs"), filepath.Join(home, "uploads")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Config{}, fmt.Errorf("config: create %s: %w", dir, err)
		}
	}
	if listen == "" {
		listen = fmt.Sprintf("127.0.0.1:%d", DefaultPort)
	}
	if noAuth && !isLoopback(listen) {
		return Config{}, errors.New("config: --no-auth needs a loopback listen address")
	}
	token := strings.TrimSpace(os.Getenv("T3GO_TOKEN"))
	if token == "" {
		token, err = readOrCreateToken(filepath.Join(home, "token"))
		if err != nil {
			return Config{}, err
		}
	}
	return Config{Home: home, Listen: listen, Token: token, NoAuth: noAuth}, nil
}

func isLoopback(listen string) bool {
	host := listen
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		host = listen[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "localhost" || host == "::1" || host == ""
}

func readOrCreateToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if token := strings.TrimSpace(string(b)); len(token) >= 20 {
			return token, nil
		}
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("config: token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("config: write token: %w", err)
	}
	return token, nil
}

// TokenPath is where a machine keeps its token.
func TokenPath(home string) string { return filepath.Join(home, "token") }

// UploadsDir is where prompt attachments are stored.
func UploadsDir(home string) string { return filepath.Join(home, "uploads") }

// DBPath is the environment's SQLite file.
func DBPath(home string) string { return filepath.Join(home, "t3go.sqlite") }

// DataDirName is the app's directory name on each platform's data path.
func DataDirName() string {
	if runtime.GOOS == "windows" {
		return "T3Go"
	}
	return "t3go"
}
