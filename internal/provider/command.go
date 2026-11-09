package provider

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// commandOutput runs path with args and returns stdout, bounded by a
// short timeout.
func commandOutput(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && len(exit.Stderr) > 0 {
			return "", &commandError{msg: strings.TrimSpace(string(exit.Stderr))}
		}
		return "", err
	}
	return string(out), nil
}

type commandError struct{ msg string }

func (e *commandError) Error() string { return e.msg }
