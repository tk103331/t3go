package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeCLI(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProviderSetupReportsAuthAndCommands(t *testing.T) {
	dir := t.TempDir()
	writeFakeCLI(t, dir, "claude", `case "$1" in
--version) echo 1.0.0 ;;
auth) echo "Not logged in" >&2; exit 1 ;;
esac`)
	writeFakeCLI(t, dir, "codex", `case "$1" in
--version) echo 2.0.0 ;;
login) echo "Logged in using ChatGPT"; exit 0 ;;
esac`)
	writeFakeCLI(t, dir, "opencode", `case "$1" in
--version) echo 3.0.0 ;;
auth) echo '[]' ;;
esac`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx := context.Background()
	claude := (&Claude{}).Snapshot(ctx)
	if !claude.Installed || claude.AuthStatus != "unauthenticated" {
		t.Fatalf("claude = %#v", claude)
	}
	if !claude.CanInstall || !claude.CanAuthenticate || claude.InstallCommand == "" || claude.LoginCommand != "claude auth login" {
		t.Fatalf("claude setup = %#v", claude)
	}
	codex := (&Codex{}).Snapshot(ctx)
	if codex.AuthStatus != "authenticated" || codex.LoginCommand != "codex login" {
		t.Fatalf("codex = %#v", codex)
	}
	opencode := (&OpenCode{}).Snapshot(ctx)
	if opencode.AuthStatus != "unauthenticated" || opencode.LoginCommand != "opencode auth login" {
		t.Fatalf("opencode = %#v", opencode)
	}
}

func TestProbeAuthUnknownCommand(t *testing.T) {
	path := writeFakeCLI(t, t.TempDir(), "old", `echo "unknown command: auth" >&2; exit 1`)
	status, detail := probeAuth(context.Background(), path, "auth", "status")
	if status != "unknown" || !strings.Contains(detail, "unknown command") {
		t.Fatalf("status = %q, detail = %q", status, detail)
	}
}
