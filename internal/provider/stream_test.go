package provider

import (
	"strings"
	"testing"

	"github.com/tk/t3go/internal/proto"
)

// recordingSink collects stream output for assertions.
type recordingSink struct {
	texts     []string
	reasoning []string
	sessions  []string
	usages    []proto.Usage
	notices   []string
	tools     map[string]*recordedTool
	order     []string
}

type recordedTool struct {
	name   string
	detail string
	output string
	failed bool
	done   bool
}

func newRecordingSink() *recordingSink {
	return &recordingSink{tools: map[string]*recordedTool{}}
}

func (r *recordingSink) Text(delta string) { r.texts = append(r.texts, delta) }

func (r *recordingSink) Reasoning(delta string) { r.reasoning = append(r.reasoning, delta) }

func (r *recordingSink) ToolStart(id, name, detail string) {
	r.tools[id] = &recordedTool{name: name, detail: detail}
	r.order = append(r.order, "start:"+id)
}

func (r *recordingSink) ToolFinish(id, output string, failed bool) {
	t := r.tools[id]
	if t == nil {
		t = &recordedTool{}
		r.tools[id] = t
	}
	t.output, t.failed, t.done = output, failed, true
	r.order = append(r.order, "finish:"+id)
}

func (r *recordingSink) Session(id string) { r.sessions = append(r.sessions, id) }

func (r *recordingSink) Usage(usage proto.Usage) { r.usages = append(r.usages, usage) }

func (r *recordingSink) Notice(message string) { r.notices = append(r.notices, message) }

func feed(t *testing.T, line func([]byte), lines ...string) {
	t.Helper()
	for _, l := range lines {
		line([]byte(l))
	}
}

func TestClaudeStreamTextToolAndUsage(t *testing.T) {
	sink := newRecordingSink()
	s := newClaudeStream(sink, "old")
	feed(t, s.line,
		`{"type":"system","subtype":"init","session_id":"sess-1"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":[{"type":"text","text":"a.go"}],"is_error":false}]}}`,
		`{"type":"result","subtype":"success","session_id":"sess-1","usage":{"input_tokens":10,"cache_read_input_tokens":5,"cache_creation_input_tokens":2,"output_tokens":7},"total_cost_usd":0.25,"result":"Hello"}`,
	)

	if got := strings.Join(sink.texts, ""); got != "Hello" {
		t.Fatalf("text = %q, want %q", got, "Hello")
	}
	if len(sink.reasoning) != 1 || sink.reasoning[0] != "hmm" {
		t.Fatalf("reasoning = %v", sink.reasoning)
	}
	if len(sink.sessions) != 1 || sink.sessions[0] != "sess-1" {
		t.Fatalf("sessions = %v", sink.sessions)
	}
	tool := sink.tools["tool-1"]
	if tool == nil || tool.name != "Bash" || !tool.done || tool.failed || tool.output != "a.go" {
		t.Fatalf("tool = %+v", tool)
	}
	if !strings.Contains(tool.detail, `"command":"ls"`) {
		t.Fatalf("tool detail = %q", tool.detail)
	}
	want := proto.Usage{InputTokens: 17, OutputTokens: 7, TotalTokens: 24, CostUSD: 0.25}
	if len(sink.usages) != 1 || sink.usages[0] != want {
		t.Fatalf("usage = %+v, want %+v", sink.usages, want)
	}
	if !s.gotText || !s.gotResult || s.resultError != "" {
		t.Fatalf("state = text:%v result:%v err:%q", s.gotText, s.gotResult, s.resultError)
	}
}

func TestClaudeStreamWholeAssistantMessageWithoutPartials(t *testing.T) {
	sink := newRecordingSink()
	s := newClaudeStream(sink, "")
	feed(t, s.line,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Finished."}]}}`,
		`{"type":"result","subtype":"success","result":"Finished."}`,
	)
	if len(sink.texts) != 1 || sink.texts[0] != "Finished." {
		t.Fatalf("texts = %v", sink.texts)
	}
}

func TestClaudeStreamResultError(t *testing.T) {
	sink := newRecordingSink()
	s := newClaudeStream(sink, "")
	feed(t, s.line,
		`{"type":"result","subtype":"error","is_error":true,"errors":[{"type":"text","text":"rate limited"},{"type":"text","text":"try later"}]}`,
	)
	if s.resultError != "rate limited\ntry later" {
		t.Fatalf("resultError = %q", s.resultError)
	}
}

func TestClaudeStreamErrorEvent(t *testing.T) {
	sink := newRecordingSink()
	s := newClaudeStream(sink, "")
	feed(t, s.line,
		`{"type":"error","error":{"message":"model unavailable"}}`,
	)
	if s.resultError != "model unavailable" {
		t.Fatalf("resultError = %q", s.resultError)
	}
}

func TestCodexStreamItemsAndUsage(t *testing.T) {
	sink := newRecordingSink()
	s := newCodexStream(sink)
	feed(t, s.line,
		`{"type":"thread.started","thread_id":"thread-9"}`,
		`{"type":"item.completed","item":{"id":"msg-1","type":"agent_message","text":"Done."}}`,
		`{"type":"item.started","item":{"id":"cmd-1","type":"command_execution","command":"go test ./..."}}`,
		`{"type":"item.completed","item":{"id":"cmd-1","type":"command_execution","command":"go test ./...","exit_code":1,"aggregated_output":"FAIL"}}`,
		`{"type":"item.completed","item":{"id":"think-1","type":"reasoning","text":"plan"}}`,
		`{"type":"item.completed","item":{"id":"search-1","type":"web_search","query":"nsgo"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":30,"reasoning_output_tokens":5}}`,
	)

	if len(sink.sessions) != 1 || sink.sessions[0] != "thread-9" {
		t.Fatalf("sessions = %v", sink.sessions)
	}
	if len(sink.texts) != 1 || sink.texts[0] != "Done." {
		t.Fatalf("texts = %v", sink.texts)
	}
	if len(sink.reasoning) != 1 || sink.reasoning[0] != "plan" {
		t.Fatalf("reasoning = %v", sink.reasoning)
	}
	tool := sink.tools["cmd-1"]
	if tool == nil || tool.name != "Shell" || tool.detail != "go test ./..." || !tool.done || !tool.failed || tool.output != "FAIL" {
		t.Fatalf("tool = %+v", tool)
	}
	if got, want := sink.order, []string{"start:cmd-1", "finish:cmd-1"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v", got)
	}
	if len(sink.notices) != 1 || sink.notices[0] != "Web search: nsgo" {
		t.Fatalf("notices = %v", sink.notices)
	}
	want := proto.Usage{InputTokens: 120, OutputTokens: 35, TotalTokens: 155}
	if len(sink.usages) != 1 || sink.usages[0] != want {
		t.Fatalf("usage = %+v, want %+v", sink.usages, want)
	}
}

func TestCodexStreamTurnFailed(t *testing.T) {
	sink := newRecordingSink()
	s := newCodexStream(sink)
	feed(t, s.line, `{"type":"turn.failed","error":{"message":"quota exceeded"}}`)
	if s.failText != "quota exceeded" {
		t.Fatalf("failText = %q", s.failText)
	}
}

func TestOpenCodeStreamDedupesPartsAndTracksTool(t *testing.T) {
	sink := newRecordingSink()
	s := newOpenCodeStream(sink)
	feed(t, s.line,
		`{"type":"text","sessionID":"oc-1","part":{"id":"p1","text":"All "}}`,
		`{"type":"text","sessionID":"oc-1","part":{"id":"p1","text":"All "}}`,
		`{"type":"text","sessionID":"oc-1","part":{"id":"p2","text":"done."}}`,
		`{"type":"reasoning","sessionID":"oc-1","part":{"id":"r1","text":"thinking"}}`,
		`{"type":"tool","sessionID":"oc-1","part":{"id":"t1","tool":"bash","callID":"call-1","state":{"status":"running","input":{"cmd":"ls"}}}}`,
		`{"type":"tool","sessionID":"oc-1","part":{"id":"t1","tool":"bash","callID":"call-1","state":{"status":"completed","title":"ls","output":"a.go"}}}`,
		`{"type":"step_finish","sessionID":"oc-1","part":{"tokens":{"input":11,"output":4},"cost":0.02}}`,
	)

	if len(sink.sessions) != 1 || sink.sessions[0] != "oc-1" {
		t.Fatalf("sessions = %v", sink.sessions)
	}
	if got := strings.Join(sink.texts, ""); got != "All done." {
		t.Fatalf("text = %q", got)
	}
	if len(sink.reasoning) != 1 || sink.reasoning[0] != "thinking" {
		t.Fatalf("reasoning = %v", sink.reasoning)
	}
	tool := sink.tools["call-1"]
	if tool == nil || tool.name != "bash" || tool.detail != `{"cmd":"ls"}` || !tool.done || tool.failed || tool.output != "a.go" {
		t.Fatalf("tool = %+v", tool)
	}
	want := proto.Usage{InputTokens: 11, OutputTokens: 4, TotalTokens: 15, CostUSD: 0.02}
	if len(sink.usages) != 1 || sink.usages[0] != want {
		t.Fatalf("usage = %+v, want %+v", sink.usages, want)
	}
}

func TestOpenCodeStreamError(t *testing.T) {
	sink := newRecordingSink()
	s := newOpenCodeStream(sink)
	feed(t, s.line,
		`{"type":"error","sessionID":"oc-1","error":{"message":"provider auth missing"}}`,
	)
	if s.failText != "provider auth missing" {
		t.Fatalf("failText = %q", s.failText)
	}

	s = newOpenCodeStream(sink)
	feed(t, s.line, `{"type":"error","sessionID":"oc-1"}`)
	if !strings.Contains(s.failText, "OpenCode failed") {
		t.Fatalf("failText = %q", s.failText)
	}
}

func TestTextOf(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string", "hello", "hello"},
		{"blocks", []any{map[string]any{"type": "text", "text": "a"}, map[string]any{"type": "text", "text": "b"}}, "ab"},
		{"nil", nil, ""},
		{"number", float64(3), "3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := textOf(c.in); got != c.want {
				t.Fatalf("textOf(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestClaudeStreamReportsContextWindow(t *testing.T) {
	sink := newRecordingSink()
	s := newClaudeStream(sink, "")
	feed(t, s.line,
		`{"type":"result","subtype":"success","usage":{"input_tokens":10,"output_tokens":2},"modelUsage":{"claude-sonnet-5-5":{"inputTokens":10,"outputTokens":2,"contextWindow":200000}}}`,
	)
	if len(sink.usages) != 1 || sink.usages[0].ContextWindow != 200_000 {
		t.Fatalf("usages = %#v", sink.usages)
	}
}

func TestCodexStreamReportsContextWindow(t *testing.T) {
	sink := newRecordingSink()
	s := newCodexStream(sink)
	feed(t, s.line,
		`{"type":"token_count","info":{"model_context_window":258400}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}`,
	)
	if len(sink.usages) != 1 || sink.usages[0].ContextWindow != 258400 {
		t.Fatalf("usages = %#v", sink.usages)
	}
}
