//go:build !(js && wasm)

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// localServerPort is the first port the local environment server tries.
const localServerPort = 4870

// bundledServerPath is the environment server inside the installed
// package: T3Go.app/Contents/Resources/t3go on macOS, t3go beside the
// client binary elsewhere. T3GO_SERVER_BIN overrides it.
func bundledServerPath() string {
	if path := strings.TrimSpace(os.Getenv("T3GO_SERVER_BIN")); path != "" {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	var path string
	switch runtime.GOOS {
	case "darwin":
		path = filepath.Join(dir, "..", "Resources", "t3go")
	case "windows":
		path = filepath.Join(dir, "t3go.exe")
	default:
		path = filepath.Join(dir, "t3go")
	}
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return ""
	}
	return path
}

// localServerHome is where the local environment server keeps its data:
// the same directory a separately installed server uses, so both see one
// workspace.
func localServerHome() string {
	if dir := strings.TrimSpace(os.Getenv("T3GO_HOME")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".t3go"
	}
	return filepath.Join(home, ".t3go")
}

// readLocalToken reads the token the local environment server keeps.
func readLocalToken(home string) string {
	raw, err := os.ReadFile(filepath.Join(home, "token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// startLocalServer starts the packaged environment server and returns its
// address and token. A server already serving the same data directory is
// reused instead of a second one.
func startLocalServer(ctx context.Context) (*localServer, error) {
	bin := bundledServerPath()
	if bin == "" {
		return nil, errNoBundledServer
	}
	home := localServerHome()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	token := readLocalToken(home)
	for port := localServerPort; port < localServerPort+20; port++ {
		base := fmt.Sprintf("http://127.0.0.1:%d", port)
		if serverAnswers(base) {
			// Another environment server holds the port: reuse it when it
			// keeps this data directory, else take the next port.
			if serverAcceptsToken(base, token) {
				// A server left behind by a client that was killed stops
				// once its parent watch notices: wait it out instead of
				// connecting to one that is about to close.
				time.Sleep(1200 * time.Millisecond)
				if serverAcceptsToken(base, token) {
					return &localServer{baseURL: base, token: token, home: home}, nil
				}
			}
			continue
		}
		return launchLocalServer(ctx, bin, home, port)
	}
	return nil, errors.New("no free port for the local environment server")
}

// launchLocalServer runs the packaged server and waits for it to answer.
func launchLocalServer(ctx context.Context, bin, home string, port int) (*localServer, error) {
	logs := filepath.Join(home, "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(logs, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	cmd := exec.Command(bin, "serve", "--home", home, "--listen", fmt.Sprintf("127.0.0.1:%d", port),
		"--parent-pid", strconv.Itoa(os.Getpid()), "--quiet")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	go func() {
		<-ctx.Done()
		stop()
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			stop()
			return nil, ctx.Err()
		}
		if serverAnswers(base) {
			if token := readLocalToken(home); token != "" {
				return &localServer{baseURL: base, token: token, home: home, stop: stop}, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()
	return nil, errors.New("the local environment server did not start")
}
