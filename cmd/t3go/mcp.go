package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// cmdMCP is the stdio MCP server a supervised provider runs to ask the
// environment's person about a tool call.
func cmdMCP(args []string) int {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	baseURL := fs.String("url", "", "environment URL")
	token := fs.String("token", "", "bearer token")
	threadID := fs.String("thread", "", "thread id")
	runID := fs.String("run", "", "run id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *baseURL == "" || *threadID == "" {
		fmt.Fprintln(os.Stderr, "t3go mcp: --url and --thread are required")
		return 2
	}
	client := &approvalClient{baseURL: strings.TrimRight(*baseURL, "/"), token: *token, threadID: *threadID, runID: *runID}
	if err := serveMCP(os.Stdin, os.Stdout, client); err != nil {
		fmt.Fprintf(os.Stderr, "t3go mcp: %v\n", err)
		return 1
	}
	return 0
}

// approvalClient asks the environment.
type approvalClient struct {
	baseURL, token, threadID, runID string
}

func (c *approvalClient) approve(ctx context.Context, tool string, input map[string]any) (proto.ApprovalDecision, error) {
	var decision proto.ApprovalDecision
	body, err := json.Marshal(map[string]any{"runId": c.runID, "tool": tool, "input": input})
	if err != nil {
		return decision, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/threads/"+url.PathEscape(c.threadID)+"/approvals", bytes.NewReader(body))
	if err != nil {
		return decision, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return decision, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return decision, fmt.Errorf("approval: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&decision); err != nil {
		return decision, err
	}
	return decision, nil
}

// rpcMessage is one JSON-RPC request or response.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// serveMCP answers the provider's MCP frames until in ends.
func serveMCP(in io.Reader, out io.Writer, client *approvalClient) error {
	reader := bufio.NewScanner(in)
	reader.Buffer(make([]byte, 0, 64<<10), 8<<20)
	writer := bufio.NewWriter(out)
	defer writer.Flush()

	reply := func(id json.RawMessage, result any, rpcErr *rpcError) error {
		message := rpcMessage{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr}
		if rpcErr != nil {
			message.Result = nil
		}
		raw, err := json.Marshal(message)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(raw, '\n')); err != nil {
			return err
		}
		return writer.Flush()
	}

	for reader.Scan() {
		line := bytes.TrimSpace(reader.Bytes())
		if len(line) == 0 {
			continue
		}
		var request rpcMessage
		if err := json.Unmarshal(line, &request); err != nil {
			continue
		}
		if len(request.ID) == 0 {
			// Notifications, such as initialized or cancelled.
			continue
		}
		switch request.Method {
		case "initialize":
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(request.Params, &params)
			protocol := params.ProtocolVersion
			if protocol == "" {
				protocol = "2024-11-05"
			}
			err := reply(request.ID, map[string]any{
				"protocolVersion": protocol,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "t3go", "version": version},
			}, nil)
			if err != nil {
				return err
			}
		case "ping":
			if err := reply(request.ID, map[string]any{}, nil); err != nil {
				return err
			}
		case "tools/list":
			if err := reply(request.ID, map[string]any{"tools": []map[string]any{{
				"name":        "approve",
				"description": "Ask the person running this T3Go environment to allow or deny a tool call.",
				"inputSchema": map[string]any{"type": "object", "additionalProperties": true},
			}}}, nil); err != nil {
				return err
			}
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(request.Params, &params)
			if params.Name != "approve" {
				if err := reply(request.ID, nil, &rpcError{Code: -32602, Message: "unknown tool: " + params.Name}); err != nil {
					return err
				}
				continue
			}
			tool := stringOf(params.Arguments["tool_name"])
			if tool == "" {
				tool = stringOf(params.Arguments["tool"])
			}
			if tool == "" {
				tool = "tool"
			}
			input, _ := params.Arguments["input"].(map[string]any)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			decision, err := client.approve(ctx, tool, input)
			cancel()
			if err != nil {
				decision = proto.ApprovalDecision{Message: err.Error()}
			}
			answer := map[string]any{"behavior": "deny", "message": decision.Message}
			if decision.Allow {
				answer = map[string]any{"behavior": "allow"}
				if input != nil {
					answer["updatedInput"] = input
				}
			}
			raw, _ := json.Marshal(answer)
			if err := reply(request.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": string(raw)}},
				"isError": false,
			}, nil); err != nil {
				return err
			}
		default:
			if err := reply(request.ID, nil, &rpcError{Code: -32601, Message: "unknown method: " + request.Method}); err != nil {
				return err
			}
		}
	}
	return reader.Err()
}

func stringOf(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}
