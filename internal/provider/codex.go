package provider

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/tk/t3go/internal/proto"
)

// Codex runs the OpenAI Codex CLI in its JSON exec mode.
type Codex struct{}

func (*Codex) ID() string   { return "codex" }
func (*Codex) Name() string { return "Codex" }

// codexModels are Codex's current models, from its catalog.
var codexModels = []proto.Model{
	{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol", Provider: "codex", Default: true},
	{ID: "gpt-6-astra", Name: "GPT-6 Astra", Provider: "codex"},
	{ID: "gpt-6-luna", Name: "GPT-6 Luna", Provider: "codex"},
}

func (*Codex) Snapshot(ctx context.Context) proto.Provider {
	p := detect(ctx, "codex", "Codex", "codex")
	p.Models = codexModels
	return p
}

func (c *Codex) Run(ctx context.Context, turn Turn, sink Sink) error {
	path, err := lookPath("codex")
	if err != nil {
		return ErrNotInstalled
	}
	args := []string{"exec"}
	resume := turn.SessionID != ""
	if resume {
		args = append(args, "resume", "--json", "--skip-git-repo-check")
	} else {
		args = append(args, "--json", "--skip-git-repo-check", "-C", turn.CWD)
	}
	if turn.Model != "" {
		args = append(args, "-m", turn.Model)
	}
	if turn.Plan {
		args = append(args, "-s", "read-only")
	}
	switch turn.Permission {
	case proto.PermissionFullAccess:
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	case proto.PermissionAuto, proto.PermissionAcceptEdits:
		if !resume {
			args = append(args, "-s", "workspace-write", "--approve-for-me")
		}
	default:
		if !resume {
			args = append(args, "-s", "workspace-write")
		}
	}
	if resume {
		args = append(args, turn.SessionID)
	}
	args = append(args, "-")

	s := newCodexStream(sink)
	res := runJSON(ctx, procSpec{Path: path, Args: args, CWD: turn.CWD}, turn.Prompt, s.line)
	if errors.Is(res.Err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	if res.Err != nil {
		return res.Err
	}
	if s.failText != "" {
		return errors.New(s.failText)
	}
	if !s.gotText {
		return errors.New("Codex returned no message")
	}
	return nil
}

// codexStream decodes Codex's JSONL exec output.
type codexStream struct {
	sink      Sink
	failText  string
	gotText   bool
	started   map[string]bool
	lastAgent string
}

func newCodexStream(sink Sink) *codexStream {
	return &codexStream{sink: sink, started: map[string]bool{}}
}

func (s *codexStream) line(line []byte) {
	var ev map[string]any
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	switch jsonString(ev, "type") {
	case "thread.started":
		if id := jsonString(ev, "thread_id"); id != "" {
			s.sink.Session(id)
		}
	case "turn.started":
	case "item.started", "item.updated", "item.completed":
		item := jsonMap(ev, "item")
		typ := jsonString(item, "type")
		id := jsonString(item, "id")
		completed := jsonString(ev, "type") == "item.completed"
		switch typ {
		case "agent_message":
			text := jsonString(item, "text")
			if completed && text != "" && text != s.lastAgent {
				s.lastAgent, s.gotText = text, true
				s.sink.Text(text)
			}
		case "reasoning":
			if completed {
				if text := jsonString(item, "text"); text != "" {
					s.sink.Reasoning(text)
				}
			}
		case "command_execution":
			command := jsonString(item, "command")
			if !s.started[id] {
				s.started[id] = true
				s.sink.ToolStart(id, "Shell", command)
			}
			if completed {
				code := int(jsonNumber(item, "exit_code"))
				output := jsonString(item, "aggregated_output")
				if output == "" {
					output = jsonString(item, "command")
				}
				s.sink.ToolFinish(id, output, code != 0)
			}
		case "file_change":
			if !s.started[id] {
				s.started[id] = true
				s.sink.ToolStart(id, "Edit", marshalDetail(item["changes"]))
			}
			if completed {
				s.sink.ToolFinish(id, jsonString(item, "status"), false)
			}
		case "mcp_tool_call":
			name := jsonString(item, "tool")
			if name == "" {
				name = jsonString(item, "name")
			}
			if !s.started[id] {
				s.started[id] = true
				s.sink.ToolStart(id, name, marshalDetail(item["arguments"]))
			}
			if completed {
				status := jsonString(item, "status")
				s.sink.ToolFinish(id, marshalDetail(item["result"]), status == "failed" || status == "error")
			}
		case "web_search":
			if completed {
				if q := jsonString(item, "query"); q != "" {
					s.sink.Notice("Web search: " + q)
				}
			}
		case "todo_list":
			if completed {
				s.sink.Notice("Plan updated")
			}
		}
	case "turn.completed":
		u := jsonMap(ev, "usage")
		s.sink.Usage(proto.Usage{
			InputTokens:  int64(jsonNumber(u, "input_tokens") + jsonNumber(u, "cached_input_tokens")),
			OutputTokens: int64(jsonNumber(u, "output_tokens") + jsonNumber(u, "reasoning_output_tokens")),
			TotalTokens:  int64(jsonNumber(u, "input_tokens") + jsonNumber(u, "cached_input_tokens") + jsonNumber(u, "output_tokens") + jsonNumber(u, "reasoning_output_tokens")),
		})
	case "turn.failed":
		s.failText = jsonString(ev, "message")
		if s.failText == "" {
			s.failText = jsonString(jsonMap(ev, "error"), "message")
		}
		if s.failText == "" {
			s.failText = "Codex could not complete the turn"
		}
	case "error":
		s.failText = jsonString(ev, "message")
		if s.failText == "" {
			s.failText = jsonString(jsonMap(ev, "error"), "message")
		}
	}
}
