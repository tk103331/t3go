package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// Pi runs the Pi coding agent through its native RPC mode.
type Pi struct {
	Settings proto.PiProviderSettings
}

func (*Pi) ID() string   { return "pi" }
func (*Pi) Name() string { return "Pi" }

func (p *Pi) binary() (string, error) {
	path := strings.TrimSpace(p.Settings.BinaryPath)
	if path == "" {
		path = "pi"
	}
	if strings.ContainsAny(path, `/\\`) {
		return path, nil
	}
	return lookPath(path)
}

func (p *Pi) args(ephemeral bool, session string) ([]string, error) {
	args := []string{"--mode", "rpc"}
	if ephemeral {
		args = append(args, "--no-session", "--offline")
	} else if session != "" {
		args = append(args, "--session", session)
	}
	for _, arg := range p.Settings.LaunchArgs {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		switch arg {
		case "--mode", "-p", "--print", "--continue", "-c", "--resume", "-r", "--session", "--session-id", "--no-session":
			return nil, fmt.Errorf("Pi launch argument %q is managed by T3Go", arg)
		}
		if strings.HasPrefix(arg, "--mode=") || strings.HasPrefix(arg, "--session=") {
			return nil, fmt.Errorf("Pi launch argument %q is managed by T3Go", arg)
		}
		args = append(args, arg)
	}
	return args, nil
}

func (p *Pi) Snapshot(ctx context.Context) proto.Provider {
	path, err := p.binary()
	if err != nil {
		return proto.Provider{ID: "pi", Name: "Pi", Source: "pi", Enabled: p.Settings.Enabled,
			Error: "not installed; install @earendil-works/pi-coding-agent", CanInstall: true,
			InstallCommand: "npm install -g --ignore-scripts @earendil-works/pi-coding-agent"}
	}
	snapshot := proto.Provider{ID: "pi", Name: "Pi", Source: "pi", Enabled: p.Settings.Enabled,
		Installed: true, Path: path, Version: versionOf(ctx, path), CanInstall: true,
		InstallCommand:  "npm install -g --ignore-scripts @earendil-works/pi-coding-agent",
		CanAuthenticate: true, LoginCommand: "pi", AuthStatus: "unknown"}
	if !p.Settings.Enabled {
		snapshot.Installed = false
		snapshot.Error = "disabled in environment settings"
		return snapshot
	}
	args, err := p.args(true, "")
	if err != nil {
		snapshot.Installed, snapshot.Error = false, err.Error()
		return snapshot
	}
	peer, err := startJSONLPeer(ctx, procSpec{Path: path, Args: args}, nil)
	if err != nil {
		snapshot.Installed, snapshot.Error = false, "could not start Pi RPC: "+err.Error()
		return snapshot
	}
	defer peer.close()
	state, err := piRequest(ctx, peer, map[string]any{"type": "get_state"}, 20*time.Second)
	if err != nil {
		snapshot.Installed, snapshot.Error = false, "could not read Pi state: "+err.Error()
		return snapshot
	}
	models, err := piRequest(ctx, peer, map[string]any{"type": "get_available_models"}, 20*time.Second)
	if err != nil {
		snapshot.Error = "could not list Pi models: " + err.Error()
		return snapshot
	}
	inventory, _ := models["models"].([]any)
	active := jsonMap(state, "model")
	activeProvider, activeID := jsonString(active, "provider"), jsonString(active, "id")
	for _, raw := range inventory {
		model := valueMap(raw)
		providerID, modelID := jsonString(model, "provider"), jsonString(model, "id")
		if providerID == "" || modelID == "" {
			continue
		}
		id := providerID + "/" + modelID
		name := jsonString(model, "name")
		if name == "" {
			name = id
		}
		snapshot.Models = append(snapshot.Models, proto.Model{ID: id, Name: name, Provider: "pi",
			Default:       providerID == activeProvider && modelID == activeID,
			ContextWindow: int(jsonNumber(model, "contextWindow"))})
	}
	if len(snapshot.Models) == 0 {
		snapshot.Models = []proto.Model{{ID: "default", Name: "Pi default", Provider: "pi", Default: true}}
		snapshot.AuthStatus = "unauthenticated"
		snapshot.AuthDetail = "Pi reported no authenticated models; run `pi` and `/login`."
	} else {
		snapshot.AuthStatus = "authenticated"
	}
	return snapshot
}

func piRequest(ctx context.Context, peer *jsonlPeer, payload map[string]any, timeout time.Duration) (map[string]any, error) {
	response, err := peer.request(ctx, payload, timeout)
	if err != nil {
		return nil, err
	}
	if success, _ := response["success"].(bool); !success {
		detail := valueText(response["error"])
		if detail == "" {
			detail = "Pi rejected " + fmt.Sprint(payload["type"])
		}
		return nil, errors.New(detail)
	}
	data, _ := response["data"].(map[string]any)
	return data, nil
}

func (p *Pi) Run(ctx context.Context, turn Turn, sink Sink) error {
	if !p.Settings.Enabled {
		return errors.New("Pi is disabled in this environment's provider settings")
	}
	path, err := p.binary()
	if err != nil {
		return ErrNotInstalled
	}
	args, err := p.args(false, turn.SessionID)
	if err != nil {
		return err
	}
	settled := make(chan struct{})
	promptFailure := make(chan string, 1)
	var once sync.Once
	var textMu sync.Mutex
	gotText, gotReasoning := false, false
	toolOpen := map[string]bool{}
	handle := func(event map[string]any) {
		switch jsonString(event, "type") {
		case "message_update":
			delta := jsonMap(event, "assistantMessageEvent")
			switch jsonString(delta, "type") {
			case "text_delta":
				if text := jsonString(delta, "delta"); text != "" {
					textMu.Lock()
					gotText = true
					textMu.Unlock()
					sink.Text(text)
				}
			case "thinking_delta":
				if text := jsonString(delta, "delta"); text != "" {
					textMu.Lock()
					gotReasoning = true
					textMu.Unlock()
					sink.Reasoning(text)
				}
			}
		case "message_end":
			message := jsonMap(event, "message")
			if jsonString(message, "role") != "assistant" {
				return
			}
			if jsonString(message, "stopReason") == "error" {
				msg := jsonString(message, "errorMessage")
				if msg == "" {
					msg = "Pi reported a model error"
				}
				select {
				case promptFailure <- msg:
				default:
				}
			}
			if usage := jsonMap(message, "usage"); usage != nil {
				in := int64(jsonNumber(usage, "input"))
				out := int64(jsonNumber(usage, "output"))
				total := int64(jsonNumber(usage, "totalTokens"))
				if total == 0 {
					total = in + out
				}
				sink.Usage(proto.Usage{InputTokens: in, OutputTokens: out, TotalTokens: total,
					CostUSD: jsonNumber(usage, "cost")})
			}
		case "tool_execution_start":
			id := jsonString(event, "toolCallId")
			if id == "" {
				id = jsonString(event, "id")
			}
			name := jsonString(event, "toolName")
			if name == "" {
				name = "tool"
			}
			toolOpen[id] = true
			sink.ToolStart(id, name, marshalDetail(event["args"]))
		case "tool_execution_end":
			id := jsonString(event, "toolCallId")
			if id == "" {
				id = jsonString(event, "id")
			}
			output := valueText(event["result"])
			failed, _ := event["isError"].(bool)
			sink.ToolFinish(id, output, failed)
			delete(toolOpen, id)
		case "agent_settled":
			once.Do(func() { close(settled) })
		case "error":
			msg := jsonString(event, "message")
			if msg == "" {
				msg = valueText(event["error"])
			}
			if msg != "" {
				select {
				case promptFailure <- msg:
				default:
				}
			}
		}
	}
	peer, err := startJSONLPeer(ctx, procSpec{Path: path, Args: args, CWD: turn.CWD}, handle)
	if err != nil {
		return err
	}
	defer peer.close()
	state, err := piRequest(ctx, peer, map[string]any{"type": "get_state"}, 20*time.Second)
	if err != nil {
		return err
	}
	if session := jsonString(state, "sessionFile"); session != "" && session != turn.SessionID {
		sink.Session(session)
	}
	if turn.Model != "" && turn.Model != "default" {
		provider, modelID, ok := strings.Cut(turn.Model, "/")
		if !ok || provider == "" || modelID == "" {
			return errors.New("Pi model must use provider/model format")
		}
		if _, err := piRequest(ctx, peer, map[string]any{"type": "set_model", "provider": provider, "modelId": modelID}, 20*time.Second); err != nil {
			return err
		}
	}
	prompt, err := peer.request(ctx, map[string]any{"type": "prompt", "message": turn.Prompt,
		"streamingBehavior": "steer"}, 30*time.Second)
	if err != nil {
		return err
	}
	if success, _ := prompt["success"].(bool); !success {
		return errors.New(valueText(prompt["error"]))
	}
	select {
	case <-settled:
	case detail := <-promptFailure:
		return errors.New(detail)
	case <-ctx.Done():
		return ctx.Err()
	case <-peer.done:
		peer.mu.Lock()
		err := peer.err
		peer.mu.Unlock()
		if err == nil {
			err = errors.New("Pi exited before the turn settled")
		}
		return err
	case <-time.After(30 * time.Minute):
		return errors.New("Pi turn timed out")
	}
	select {
	case detail := <-promptFailure:
		if detail != "" {
			return errors.New(detail)
		}
	default:
	}
	textMu.Lock()
	defer textMu.Unlock()
	if !gotText && !gotReasoning {
		return errors.New("Pi returned no text")
	}
	return nil
}

func valueMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func valueText(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	case nil:
		return ""
	default:
		raw, _ := json.Marshal(value)
		return string(raw)
	}
}
