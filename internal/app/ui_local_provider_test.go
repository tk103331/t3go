package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/i18n"
)

// Exercise the built-in Codex adapter, process discovery, login probe and
// JSONL conversation with a deterministic executable. No Relay exists.
func TestUILocalProviderWithoutRelay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX CLI fixture")
	}
	dir := t.TempDir()
	cli := filepath.Join(dir, "codex")
	argsLog, promptsLog := filepath.Join(dir, "args.log"), filepath.Join(dir, "prompts.log")
	script := `#!/bin/sh
case "$1" in
--version) echo "codex-cli local-test" ;;
login) echo "Logged in using ChatGPT" ;;
exec)
  printf '%s\n' "$@" >> "$LOCAL_CLI_ARGS"
  cat >> "$LOCAL_CLI_PROMPTS"
  printf '%s\n' '{"type":"thread.started","thread_id":"local-provider-session"}'
  printf '%s\n' '{"type":"item.completed","item":{"id":"reply","type":"agent_message","text":"LOCAL_PROVIDER_OK"}}'
  printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":3}}'
  ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LOCAL_CLI_ARGS", argsLog)
	t.Setenv("LOCAL_CLI_PROMPTS", promptsLog)
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	settings := srv.Service.Settings(ctx)
	settings.DefaultProvider, settings.DefaultModel = "codex", ""
	if err := srv.Service.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Service.CreateProject(ctx, "Local conversation", gitRepo(t)); err != nil {
		t.Fatal(err)
	}
	tester, session := startAppAt(t, ts, cfg, 1280, 820, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session.connected && len(session.projects) == 1
	}, "direct local connection")
	click := func(label string) {
		t.Helper()
		if err := tester.tt.Click(i18n.T(label)); err != nil {
			t.Fatalf("click %s: %v", label, err)
		}
		tester.pump()
	}
	session.openSettings()
	tester.pump()
	click("Providers")
	click("Refresh")
	tester.wait(t, 15*time.Second, func() bool { return !session.providersBusy }, "local provider refresh")
	found := false
	for _, p := range session.providers {
		if p.ID == "codex" {
			found = p.Path == cli && p.Installed && p.Enabled && p.AuthStatus == "authenticated"
		}
	}
	if !found {
		t.Fatal("Providers did not detect the local executable and its signed-in state")
	}
	click("Close settings")
	click("New thread")
	if session.newThreadProv != "codex" || session.newThreadModel != "gpt-6.1-sol" {
		t.Fatal("new thread did not select the local provider and its default model")
	}
	click("New thread message")
	tester.tt.Type("First local prompt")
	click("Create thread")
	tester.wait(t, 15*time.Second, func() bool {
		return session.detail.Thread.ID != "" && !session.running && tester.tt.HasText("LOCAL_PROVIDER_OK")
	}, "first local CLI reply")
	click("Message")
	tester.tt.Type("Second local prompt")
	click("Send")
	tester.wait(t, 15*time.Second, func() bool {
		return len(session.messages) == 4 && !session.running
	}, "second local CLI reply")
	args, err := os.ReadFile(argsLog)
	if err != nil || !strings.Contains(string(args), "resume\n") {
		t.Fatal("second turn did not resume the CLI conversation")
	}
	prompts, err := os.ReadFile(promptsLog)
	if err != nil || !strings.Contains(string(prompts), "First local prompt") || !strings.Contains(string(prompts), "Second local prompt") {
		t.Fatal("the built-in adapter did not pass both prompts to the CLI")
	}
	if tester.manager.accountSessionValid() || srv.Relay != nil {
		t.Fatal("local conversation unexpectedly used an account or Relay")
	}
	if _, err := os.Stat(filepath.Join(cfg.Home, "relay.json")); !os.IsNotExist(err) {
		t.Fatal("local conversation configured Relay")
	}
	if session.client().BaseURL != ts.URL {
		t.Fatal("conversation switched away from the direct environment route")
	}
}
