package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
)

func writeExecutableScript(t *testing.T, directory, name, contents string) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPiRPCSnapshotAndRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake executable")
	}
	path := writeExecutableScript(t, filepath.Join(t.TempDir(), "pi tools"), "pi", `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s\n' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *'"type":"get_state"'*)
      printf '{"id":"%s","success":true,"data":{"sessionFile":"pi-session","model":{"provider":"mock","id":"model"}}}\n' "$id"
      ;;
    *'"type":"get_available_models"'*)
      printf '{"id":"%s","success":true,"data":{"models":[{"provider":"mock","id":"model","name":"Mock Model","contextWindow":12345}]}}\n' "$id"
      ;;
    *'"type":"prompt"'*)
      printf '{"id":"%s","success":true,"data":{}}\n' "$id"
      printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"planning"}}'
      printf '%s\n' '{"type":"tool_execution_start","toolCallId":"tool-1","toolName":"read","args":{"path":"README.md"}}'
      printf '%s\n' '{"type":"tool_execution_end","toolCallId":"tool-1","result":{"text":"contents"},"isError":false}'
      printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello from pi"}}'
      printf '%s\n' '{"type":"message_end","message":{"role":"assistant","stopReason":"endTurn","usage":{"input":4,"output":3,"totalTokens":7,"cost":0.01}}}'
      printf '%s\n' '{"type":"agent_settled"}'
      ;;
    *)
      printf '{"id":"%s","success":true,"data":{}}\n' "$id"
      ;;
  esac
done
`)
	adapter := &Pi{Settings: proto.PiProviderSettings{Enabled: true, BinaryPath: path}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snapshot := adapter.Snapshot(ctx)
	if !snapshot.Installed || snapshot.Error != "" || len(snapshot.Models) != 1 ||
		snapshot.Models[0].ID != "mock/model" || snapshot.Models[0].ContextWindow != 12345 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	sink := newRecordingSink()
	if err := adapter.Run(ctx, Turn{CWD: t.TempDir(), Prompt: "hello", Model: "default"}, sink); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sink.texts, ""); got != "hello from pi" {
		t.Fatalf("text = %q", got)
	}
	if got := strings.Join(sink.reasoning, ""); got != "planning" {
		t.Fatalf("reasoning = %q", got)
	}
	if len(sink.sessions) != 1 || sink.sessions[0] != "pi-session" ||
		len(sink.usages) != 1 || sink.usages[0].TotalTokens != 7 ||
		sink.tools["tool-1"] == nil || !sink.tools["tool-1"].done {
		t.Fatalf("sessions=%#v usages=%#v tools=%#v", sink.sessions, sink.usages, sink.tools)
	}
}

func TestACPLocalCommandUsesLiteralArgumentsAndRespondsToPermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake executable")
	}
	root := t.TempDir()
	logPath := filepath.Join(root, "acp-args.log")
	path := writeExecutableScript(t, filepath.Join(root, "agent tools"), "fake acp", `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'fake-acp 1.0\n'
  exit 0
fi
printf '%s|%s\n' "$1" "$2" >> '`+logPath+`'
while IFS= read -r line; do
  id=$(printf '%s\n' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"protocolVersion":1,"authMethods":[]}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"sessionId":"acp-session","configOptions":[{"id":"model","name":"Model","type":"select","currentValue":"fast","options":[{"value":"fast","name":"Fast"}]}]}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking"}}}}'
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"tool-1","name":"read","title":"Read file"}}}'
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"tool-1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"done"}}]}}}'
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello from acp"}}}}'
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/request_permission","id":"permission-1","params":{"options":[{"optionId":"allow","kind":"allow_once"},{"optionId":"reject","kind":"reject_once"}]}}'
      IFS= read -r permission
      printf '%s\n' "$permission" >> '`+logPath+`'
      printf '{"jsonrpc":"2.0","id":"%s","result":{"stopReason":"endTurn"}}\n' "$id"
      ;;
  esac
done
`)
	adapter := &ACP{IDValue: "fake-acp", Home: root, Settings: proto.ACPProviderSettings{
		ID: "fake-acp", Name: "Fake ACP", Enabled: true, Source: "local",
		CommandPath: path, CommandArgs: []string{"argument with spaces", "$HOME"},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snapshot := adapter.Snapshot(ctx)
	if !snapshot.Installed || snapshot.Error != "" || len(snapshot.Models) != 1 || snapshot.Models[0].ID != "fast" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	sink := newRecordingSink()
	if err := adapter.Run(ctx, Turn{CWD: t.TempDir(), Prompt: "hello", Model: "default",
		Permission: proto.PermissionAuto}, sink); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sink.texts, ""); got != "hello from acp" {
		t.Fatalf("text = %q", got)
	}
	if got := strings.Join(sink.reasoning, ""); got != "thinking" {
		t.Fatalf("reasoning = %q", got)
	}
	if len(sink.sessions) != 1 || sink.sessions[0] != "acp-session" ||
		sink.tools["tool-1"] == nil || !sink.tools["tool-1"].done {
		t.Fatalf("sessions=%#v tools=%#v", sink.sessions, sink.tools)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 3 {
		t.Fatalf("argument log = %q", raw)
	}
	if lines[0] != "argument with spaces|$HOME" {
		t.Fatalf("literal arguments = %q", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], `"id":"permission-1"`) ||
		!strings.Contains(lines[len(lines)-1], `"optionId":"allow"`) {
		t.Fatalf("permission response = %q", lines[len(lines)-1])
	}
}
