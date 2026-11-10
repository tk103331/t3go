package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMCPApproval speaks the helper's side of the MCP protocol with a
// fake environment, checking the handshake, the tool and its decision.
func TestMCPApproval(t *testing.T) {
	var seenTool string
	env := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/threads/thr_1/approvals" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			RunID string         `json:"runId"`
			Tool  string         `json:"tool"`
			Input map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		seenTool = body.Tool
		if body.RunID != "run_1" || body.Input["command"] != "ls" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"allow": true})
	}))
	defer env.Close()

	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	client := &approvalClient{baseURL: env.URL, token: "tok", threadID: "thr_1", runID: "run_1"}
	served := make(chan error, 1)
	go func() { served <- serveMCP(inReader, outWriter, client) }()
	reader := bufio.NewReader(outReader)
	write := func(message any) {
		t.Helper()
		raw, err := json.Marshal(message)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := inWriter.Write(append(raw, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	read := func() rpcMessage {
		t.Helper()
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		return message
	}

	write(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18"}})
	init := read()
	result, _ := init.Result.(map[string]any)
	if init.Error != nil || result["protocolVersion"] != "2025-06-18" ||
		result["serverInfo"].(map[string]any)["name"] != "t3go" {
		t.Fatalf("initialize = %#v", init)
	}

	write(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	list := read()
	tools, _ := list.Result.(map[string]any)["tools"].([]any)
	if list.Error != nil || len(tools) != 1 || tools[0].(map[string]any)["name"] != "approve" {
		t.Fatalf("tools/list = %#v", list)
	}

	write(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{
		"name": "approve", "arguments": map[string]any{
			"tool_name": "Bash", "input": map[string]any{"command": "ls"},
		},
	}})
	call := read()
	if call.Error != nil || seenTool != "Bash" {
		t.Fatalf("tools/call = %#v, saw %q", call, seenTool)
	}
	content, _ := call.Result.(map[string]any)["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"behavior":"allow"`) {
		t.Fatalf("decision text = %q", text)
	}

	inWriter.Close()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	default:
	}
	_ = outReader.Close()
}
