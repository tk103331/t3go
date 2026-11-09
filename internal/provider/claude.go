package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tk/t3go/internal/proto"
)

// Claude runs Claude Code in its stream-json print mode.
//
// The CLI keeps its own authenticated session store. T3Go gives it the
// project as its working directory and resumes the conversation by the
// session id Claude reports. Permission prompts cannot be answered in
// this mode, so a supervised turn is denied anything that would prompt;
// Auto, Auto-accept edits and Full access run without asking.
type Claude struct{}

func (*Claude) ID() string   { return "claude" }
func (*Claude) Name() string { return "Claude Code" }

// claudeModels are Claude Code's current models, from its catalog.
var claudeModels = []proto.Model{
	{ID: "claude-opus-5-5", Name: "Claude Opus 5.5", Provider: "claude"},
	{ID: "claude-sonnet-5-5", Name: "Claude Sonnet 5.5", Provider: "claude", Default: true},
	{ID: "claude-fable-5-1", Name: "Claude Fable 5.1", Provider: "claude"},
	{ID: "claude-haiku-5-5", Name: "Claude Haiku 5.5", Provider: "claude"},
}

func (*Claude) Snapshot(ctx context.Context) proto.Provider {
	p := detect(ctx, "claude", "Claude Code", "claude")
	p.Models = claudeModels
	return p
}

func claudePermission(m proto.PermissionMode) (string, []string) {
	switch m {
	case proto.PermissionSupervised:
		return "manual", []string{"--permission-prompts", "none"}
	case proto.PermissionAcceptEdits:
		return "acceptEdits", nil
	case proto.PermissionAuto:
		return "auto", nil
	default:
		return "bypassPermissions", nil
	}
}

func (c *Claude) Run(ctx context.Context, turn Turn, sink Sink) error {
	path, err := lookPath("claude")
	if err != nil {
		return ErrNotInstalled
	}
	mode, extra := claudePermission(turn.Permission)
	args := []string{"--print", "--verbose", "--output-format", "stream-json", "--include-partial-messages", "--permission-mode", mode}
	args = append(args, extra...)
	if turn.Model != "" {
		args = append(args, "--model", turn.Model)
	}
	if turn.SessionID != "" {
		args = append(args, "--resume", turn.SessionID)
	}
	s := newClaudeStream(sink, turn.SessionID)
	res := runJSON(ctx, procSpec{Path: path, Args: args, CWD: turn.CWD}, turn.Prompt, s.line)
	if errors.Is(res.Err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	if res.Err != nil {
		return res.Err
	}
	if s.resultError != "" {
		return errors.New(s.resultError)
	}
	if !s.gotResult && res.Lines == 0 {
		return errors.New("Claude returned no output; check that it is signed in")
	}
	if !s.gotText {
		return errors.New("Claude returned no text")
	}
	return nil
}

// claudeStream decodes Claude Code's stream-json output.
type claudeStream struct {
	sink        Sink
	session     string
	resultError string
	gotText     bool
	gotResult   bool
	seenText    map[string]bool
}

func newClaudeStream(sink Sink, session string) *claudeStream {
	return &claudeStream{sink: sink, session: session, seenText: map[string]bool{}}
}

func (s *claudeStream) line(line []byte) {
	var ev map[string]any
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	switch jsonString(ev, "type") {
	case "system":
		if id := jsonString(ev, "session_id"); id != "" && id != s.session {
			s.session = id
			s.sink.Session(id)
		}
	case "stream_event":
		event := jsonMap(ev, "event")
		switch jsonString(event, "type") {
		case "content_block_delta":
			delta := jsonMap(event, "delta")
			switch jsonString(delta, "type") {
			case "text_delta":
				if text := jsonString(delta, "text"); text != "" {
					s.gotText = true
					s.sink.Text(text)
				}
			case "thinking_delta":
				if text := jsonString(delta, "thinking"); text != "" {
					s.sink.Reasoning(text)
				}
			}
		}
	case "assistant":
		message := jsonMap(ev, "message")
		content, _ := message["content"].([]any)
		for _, raw := range content {
			block, _ := raw.(map[string]any)
			switch jsonString(block, "type") {
			case "tool_use":
				id := jsonString(block, "id")
				name := jsonString(block, "name")
				s.sink.ToolStart(id, name, marshalDetail(block["input"]))
			case "text":
				// With partial messages this is already streamed;
				// without them it is the whole reply.
				text := jsonString(block, "text")
				if text != "" && !s.gotText && !s.seenText[text] {
					s.seenText[text], s.gotText = true, true
					s.sink.Text(text)
				}
			}
		}
	case "user":
		message := jsonMap(ev, "message")
		content, _ := message["content"].([]any)
		for _, raw := range content {
			block, _ := raw.(map[string]any)
			if jsonString(block, "type") != "tool_result" {
				continue
			}
			id := jsonString(block, "tool_use_id")
			failed, _ := block["is_error"].(bool)
			s.sink.ToolFinish(id, textOf(block["content"]), failed)
		}
	case "result":
		s.gotResult = true
		if id := jsonString(ev, "session_id"); id != "" && id != s.session {
			s.session = id
			s.sink.Session(id)
		}
		if u := jsonMap(ev, "usage"); u != nil {
			s.usage(ev, u)
		}
		if isError, _ := ev["is_error"].(bool); isError {
			s.resultError = jsonString(ev, "result")
			if s.resultError == "" {
				if list, _ := ev["errors"].([]any); len(list) > 0 {
					var b strings.Builder
					for _, e := range list {
						fmt.Fprintln(&b, textOf(e))
					}
					s.resultError = strings.TrimSpace(b.String())
				}
			}
			if s.resultError == "" {
				s.resultError = "Claude could not complete the turn"
			}
		} else if text := jsonString(ev, "result"); text != "" && !s.gotText {
			s.gotText = true
			s.sink.Text(text)
		}
	case "error":
		msg := jsonString(ev, "message")
		if msg == "" {
			msg = jsonString(jsonMap(ev, "error"), "message")
		}
		if msg != "" {
			s.resultError = msg
		}
	}
}

func (s *claudeStream) usage(ev, u map[string]any) {
	usage := proto.Usage{
		InputTokens:  int64(jsonNumber(u, "input_tokens") + jsonNumber(u, "cache_read_input_tokens") + jsonNumber(u, "cache_creation_input_tokens")),
		OutputTokens: int64(jsonNumber(u, "output_tokens")),
		CostUSD:      jsonNumber(ev, "total_cost_usd"),
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	s.sink.Usage(usage)
}

// textOf flattens a tool result, which Claude sends as a string or a list
// of content blocks.
func textOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var b strings.Builder
		for _, item := range x {
			switch y := item.(type) {
			case string:
				b.WriteString(y)
			case map[string]any:
				if t := jsonString(y, "text"); t != "" {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	case map[string]any:
		if t := jsonString(x, "text"); t != "" {
			return t
		}
		b, _ := json.Marshal(x)
		return string(b)
	default:
		if v == nil {
			return ""
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
}
