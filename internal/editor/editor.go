// Package editor opens a checkout in the person's editor.
package editor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// Choices are the editors a checkout can open in.
var Choices = []string{"auto", "zed", "code", "cursor", "finder"}

// Open starts the editor on dir and returns without waiting for it to quit.
func Open(ctx context.Context, dir, choice string) error {
	// The editor outlives the request that opened it.
	ctx = context.WithoutCancel(ctx)
	if dir == "" {
		return errors.New("this thread has no checkout")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return errors.New("the checkout does not exist")
	}
	known := false
	for _, candidate := range Choices {
		if candidate == choice {
			known = true
		}
	}
	if !known {
		return errors.New("unknown editor")
	}
	if choice == "auto" {
		switch {
		case lookPath("zed"):
			choice = "zed"
		case lookPath("code"):
			choice = "code"
		case lookPath("cursor"):
			choice = "cursor"
		default:
			choice = "finder"
		}
	}
	var cmd *exec.Cmd
	switch choice {
	case "zed":
		if lookPath("zed") {
			cmd = exec.CommandContext(ctx, "zed", dir)
			break
		}
		cmd = macApp(ctx, "Zed", dir)
	case "code":
		if lookPath("code") {
			cmd = exec.CommandContext(ctx, "code", dir)
			break
		}
		cmd = macApp(ctx, "Visual Studio Code", dir)
	case "cursor":
		if lookPath("cursor") {
			cmd = exec.CommandContext(ctx, "cursor", dir)
			break
		}
		cmd = macApp(ctx, "Cursor", dir)
	case "finder":
		cmd = folderOpener(ctx, dir)
	}
	if cmd == nil {
		return errors.New("that editor is not available on this machine")
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// macApp opens dir in a named macOS application.
func macApp(ctx context.Context, app, dir string) *exec.Cmd {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return exec.CommandContext(ctx, "open", "-a", app, dir)
}

// folderOpener opens dir in the platform's file manager.
func folderOpener(ctx context.Context, dir string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", dir)
	case "windows":
		return exec.CommandContext(ctx, "explorer", dir)
	default:
		return exec.CommandContext(ctx, "xdg-open", dir)
	}
}

// lookPath reports whether a command is on PATH.
func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
