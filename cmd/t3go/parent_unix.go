//go:build !windows

package main

import (
	"errors"
	"syscall"
)

// processAlive tells whether pid names a running process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
