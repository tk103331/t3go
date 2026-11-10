package provider

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/tk/t3go/internal/proto"
)

func TestClaudeArgsForApprovals(t *testing.T) {
	supervised := Turn{ThreadID: "thr_1", RunID: "run_1", Permission: proto.PermissionSupervised}

	args := strings.Join(claudeArgs(supervised, "/tmp/mcp.json"), " ")
	if !strings.Contains(args, "--permission-prompts host") || !strings.Contains(args, "--permission-prompt-tool mcp__t3go__approve") {
		t.Fatalf("supervised args = %q", args)
	}
	if !strings.Contains(args, "--mcp-config /tmp/mcp.json") {
		t.Fatalf("supervised args without config = %q", args)
	}

	args = strings.Join(claudeArgs(supervised, ""), " ")
	if !strings.Contains(args, "--permission-prompts none") || strings.Contains(args, "--mcp-config") {
		t.Fatalf("supervised without approvals = %q", args)
	}

	full := strings.Join(claudeArgs(Turn{Permission: proto.PermissionFullAccess}, ""), " ")
	if strings.Contains(full, "--permission-prompts") || strings.Contains(full, "--mcp-config") {
		t.Fatalf("full access args = %q", full)
	}

	plan := strings.Join(claudeArgs(Turn{Permission: proto.PermissionFullAccess, Plan: true}, ""), " ")
	if !strings.Contains(plan, "--permission-mode plan") || strings.Contains(plan, "--permission-prompts") {
		t.Fatalf("plan args = %q", plan)
	}
}

func TestWriteApprovalConfig(t *testing.T) {
	path, err := writeApprovalConfig(Turn{ThreadID: "thr_1", RunID: "run_1", ApprovalURL: "http://127.0.0.1:9", ApprovalToken: "tok"})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	defer os.Remove(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var config struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode: %v", err)
	}
	server, ok := config.Servers["t3go"]
	if !ok || server.Command == "" {
		t.Fatalf("config = %s", raw)
	}
	args := strings.Join(server.Args, " ")
	for _, want := range []string{"mcp", "--url http://127.0.0.1:9", "--token tok", "--thread thr_1", "--run run_1"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args = %q, want %q", args, want)
		}
	}
}
