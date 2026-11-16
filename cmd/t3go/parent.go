package main

import (
	"context"
	"time"
)

// watchParent stops the server when the process that started it exits, so
// the environment a desktop app ships does not outlive the app.
func watchParent(ctx context.Context, stop context.CancelFunc, pid int) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !processAlive(pid) {
					stop()
					return
				}
			}
		}
	}()
}
