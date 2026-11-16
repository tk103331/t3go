//go:build js && wasm

package main

import "context"

// The browser client is served by an environment server; it never runs
// one of its own.
func bundledServerPath() string { return "" }

func startLocalServer(ctx context.Context, background bool) (*localServer, error) {
	return nil, errNoBundledServer
}
