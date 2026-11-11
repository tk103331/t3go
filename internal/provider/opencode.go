package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/tk/t3go/internal/proto"
)

// OpenCode runs the OpenCode CLI in its JSON run mode.
type OpenCode struct{}

func (*OpenCode) ID() string   { return "opencode" }
func (*OpenCode) Name() string { return "OpenCode" }

func (*OpenCode) Snapshot(ctx context.Context) proto.Provider {
	p := detect(ctx, "opencode", "OpenCode", "opencode")
	if !p.Installed {
		return p
	}
	// `opencode models` lists the models the user's providers expose.
	out, err := commandOutput(ctx, p.Path, "models")
	if err != nil {
		p.Error = "could not list models: " + err.Error()
		return p
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		id := strings.TrimSpace(line)
		if id == "" || seen[id] || !strings.Contains(id, "/") {
			continue
		}
		seen[id] = true
		name := id
		if i := strings.LastIndex(id, "/"); i >= 0 && i+1 < len(id) {
			name = id[i+1:]
		}
		p.Models = append(p.Models, proto.Model{ID: id, Name: name, Provider: "opencode",
			Default: len(p.Models) == 0, ContextWindow: ContextWindow(id)})
	}
	if len(p.Models) == 0 {
		p.Error = "no models; run `opencode auth login`"
	}
	return p
}

func (o *OpenCode) Run(ctx context.Context, turn Turn, sink Sink) error {
	path, err := lookPath("opencode")
	if err != nil {
		return ErrNotInstalled
	}
	args := []string{"run", "--format", "json"}
	if turn.SessionID != "" {
		args = append(args, "--session", turn.SessionID)
	}
	if turn.Model != "" {
		args = append(args, "--model", turn.Model)
	}
	if turn.Plan {
		args = append(args, "--agent", "plan")
	}
	if turn.Permission == proto.PermissionAuto || turn.Permission == proto.PermissionFullAccess {
		args = append(args, "--auto")
	}
	args = append(args, turn.Prompt)

	s := newOpenCodeStream(sink)
	res := runJSON(ctx, procSpec{Path: path, Args: args, CWD: turn.CWD}, "", s.line)
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
		return errors.New("OpenCode returned no text")
	}
	return nil
}

// openCodeStream decodes OpenCode's JSON event output.
type openCodeStream struct {
	sink     Sink
	session  string
	failText string
	gotText  bool
	seenText map[string]bool
	toolOpen map[string]bool
}

func newOpenCodeStream(sink Sink) *openCodeStream {
	return &openCodeStream{sink: sink, seenText: map[string]bool{}, toolOpen: map[string]bool{}}
}

func (s *openCodeStream) line(line []byte) {
	var ev map[string]any
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	if id := jsonString(ev, "sessionID"); id != "" && id != s.session {
		s.session = id
		s.sink.Session(id)
	}
	part := jsonMap(ev, "part")
	partID := jsonString(part, "id")
	switch jsonString(ev, "type") {
	case "text":
		if text := jsonString(part, "text"); text != "" && !s.seenText[partID] {
			s.seenText[partID], s.gotText = true, true
			s.sink.Text(text)
		}
	case "reasoning":
		if text := jsonString(part, "text"); text != "" {
			s.sink.Reasoning(text)
		}
	case "tool":
		name := jsonString(part, "tool")
		callID := jsonString(part, "callID")
		if callID == "" {
			callID = partID
		}
		state := jsonMap(part, "state")
		status := jsonString(state, "status")
		if !s.toolOpen[callID] {
			s.toolOpen[callID] = true
			detail := jsonString(state, "title")
			if detail == "" {
				detail = marshalDetail(state["input"])
			}
			s.sink.ToolStart(callID, name, detail)
		}
		if status == "completed" || status == "error" || status == "failed" {
			output := textOf(state["output"])
			if output == "" {
				output = jsonString(state, "output")
			}
			s.sink.ToolFinish(callID, output, status != "completed")
		}
	case "step_finish":
		if tokens := jsonMap(part, "tokens"); tokens != nil {
			in := int64(jsonNumber(tokens, "input"))
			out := int64(jsonNumber(tokens, "output"))
			s.sink.Usage(proto.Usage{InputTokens: in, OutputTokens: out, TotalTokens: in + out, CostUSD: jsonNumber(part, "cost")})
		}
	case "error":
		s.failText = jsonString(ev, "message")
		if s.failText == "" {
			s.failText = jsonString(jsonMap(ev, "error"), "message")
		}
		if s.failText == "" {
			s.failText = "OpenCode failed: " + strconv.Quote(string(line))
		}
	}
}
