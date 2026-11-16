package main

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("this process should be alive")
	}
	if processAlive(0) || processAlive(-1) {
		t.Fatal("no process has these ids")
	}
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Skipf("a child process cannot start here: %v", err)
	}
	pid := child.Process.Pid
	_ = child.Process.Kill()
	_ = child.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("killed child %d still looks alive", pid)
	}
}

func TestWatchParentStopsWhenTheProcessExits(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Skipf("a child process cannot start here: %v", err)
	}
	reaped := false
	defer func() {
		if !reaped {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	watchParent(ctx, func() { close(stopped) }, child.Process.Pid)
	_ = child.Process.Kill()
	_ = child.Wait()
	reaped = true
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the watch did not stop the server")
	}
}
