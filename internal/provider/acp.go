package provider

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// ACP runs a generic Agent Client Protocol provider over stdio.
type ACP struct {
	IDValue  string
	Settings proto.ACPProviderSettings
	Home     string
}

func (a *ACP) ID() string { return a.IDValue }
func (a *ACP) Name() string {
	if strings.TrimSpace(a.Settings.Name) != "" {
		return a.Settings.Name
	}
	return a.IDValue
}

func (a *ACP) baseSnapshot(enabled bool) proto.Provider {
	return proto.Provider{ID: a.ID(), Name: a.Name(), Source: "acp", ConfigID: a.Settings.ID,
		Enabled: enabled, AuthStatus: "unknown"}
}

func (a *ACP) Snapshot(ctx context.Context) proto.Provider {
	p := a.baseSnapshot(a.Settings.Enabled)
	if !a.Settings.Enabled {
		p.Error = "disabled in environment settings"
		return p
	}
	spec, version, err := resolveACPCommand(ctx, a.Settings, a.Home)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	p.Path, p.Version = spec.Path, version
	if p.Version == "" {
		p.Version = versionOf(ctx, spec.Path)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	peer, err := startJSONLPeer(requestCtx, spec, nil)
	if err != nil {
		p.Error = "could not start ACP command: " + err.Error()
		return p
	}
	defer peer.close()
	init, err := acpRequest(requestCtx, peer, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "T3Go", "version": "0.1.0"},
	}, 20*time.Second)
	if err != nil {
		p.Error = "ACP initialize failed: " + err.Error()
		return p
	}
	p.Installed = true
	if methods, ok := init["authMethods"].([]any); ok {
		if len(methods) > 0 {
			p.CanAuthenticate = true
			p.AuthStatus = "unauthenticated"
			p.AuthDetail = "ACP provider advertises sign-in methods."
		} else {
			p.AuthStatus = "authenticated"
		}
	}
	cwd, _ := os.UserHomeDir()
	if cwd == "" {
		cwd = a.Home
	}
	session, err := acpRequest(requestCtx, peer, "session/new", map[string]any{
		"cwd": cwd, "mcpServers": []any{},
	}, 20*time.Second)
	if err != nil {
		p.Error = "ACP session probe failed: " + err.Error()
		return p
	}
	p.Models = acpModels(session, a.ID())
	if len(p.Models) == 0 {
		p.Models = []proto.Model{{ID: "default", Name: "Default", Provider: a.ID(), Default: true}}
	}
	return p
}

func acpModels(session map[string]any, providerID string) []proto.Model {
	options, _ := session["configOptions"].([]any)
	var models []proto.Model
	for _, raw := range options {
		option := valueMap(raw)
		id := strings.ToLower(jsonString(option, "id"))
		name := strings.ToLower(jsonString(option, "name"))
		if jsonString(option, "type") != "select" || (!strings.Contains(id, "model") && !strings.Contains(name, "model")) {
			continue
		}
		current := jsonString(option, "currentValue")
		values := acpSelectOptions(option["options"])
		for _, value := range values {
			models = append(models, proto.Model{ID: value.ID, Name: value.Name, Provider: providerID,
				Default: value.ID == current})
		}
		break
	}
	return models
}

type acpOption struct {
	ID   string
	Name string
}

func acpSelectOptions(raw any) []acpOption {
	values, _ := raw.([]any)
	var out []acpOption
	for _, value := range values {
		item := valueMap(value)
		if grouped, ok := item["options"].([]any); ok {
			out = append(out, acpSelectOptions(grouped)...)
			continue
		}
		id := jsonString(item, "value")
		if id == "" {
			id = jsonString(item, "id")
		}
		if id == "" {
			continue
		}
		name := jsonString(item, "name")
		if name == "" {
			name = id
		}
		out = append(out, acpOption{ID: id, Name: name})
	}
	return out
}

func acpRequest(ctx context.Context, peer *jsonlPeer, method string, params map[string]any, timeout time.Duration) (map[string]any, error) {
	response, err := peer.request(ctx, map[string]any{"jsonrpc": "2.0", "method": method, "params": params}, timeout)
	if err != nil {
		return nil, err
	}
	if rpcErr := valueMap(response["error"]); rpcErr != nil {
		msg := jsonString(rpcErr, "message")
		if msg == "" {
			msg = "ACP request failed"
		}
		return nil, errors.New(msg)
	}
	result := valueMap(response["result"])
	return result, nil
}

func (a *ACP) Run(ctx context.Context, turn Turn, sink Sink) error {
	if !a.Settings.Enabled {
		return errors.New("ACP provider is disabled in this environment")
	}
	spec, _, err := resolveACPCommand(ctx, a.Settings, a.Home)
	if err != nil {
		return err
	}
	spec.CWD = turn.CWD
	var readyMu sync.Mutex
	ready := false
	toolOpen := map[string]bool{}
	var peer *jsonlPeer
	handle := func(msg map[string]any) {
		method := jsonString(msg, "method")
		if method != "session/update" {
			return
		}
		params := jsonMap(msg, "params")
		update := jsonMap(params, "update")
		if update == nil {
			update = jsonMap(params, "sessionUpdate")
		}
		if update == nil {
			return
		}
		readyMu.Lock()
		streaming := ready
		readyMu.Unlock()
		kind := jsonString(update, "sessionUpdate")
		switch kind {
		case "agent_message_chunk":
			if streaming {
				if text := acpContentText(update["content"]); text != "" {
					sink.Text(text)
				}
			}
		case "agent_thought_chunk":
			if streaming {
				if text := acpContentText(update["content"]); text != "" {
					sink.Reasoning(text)
				}
			}
		case "tool_call":
			id := jsonString(update, "toolCallId")
			name := jsonString(update, "name")
			if name == "" {
				name = jsonString(update, "title")
			}
			if name == "" {
				name = "tool"
			}
			detail := jsonString(update, "title")
			if raw := update["rawInput"]; raw != nil {
				detail = marshalDetail(raw)
			}
			toolOpen[id] = true
			sink.ToolStart(id, name, detail)
		case "tool_call_update":
			id := jsonString(update, "toolCallId")
			status := jsonString(update, "status")
			if !toolOpen[id] {
				toolOpen[id] = true
				name := jsonString(update, "name")
				if name == "" {
					name = "tool"
				}
				sink.ToolStart(id, name, jsonString(update, "title"))
			}
			if status == "completed" || status == "failed" || status == "cancelled" {
				output := acpToolOutput(update)
				sink.ToolFinish(id, output, status != "completed")
				delete(toolOpen, id)
			}
		case "usage_update":
			used := int64(jsonNumber(update, "used"))
			sink.Usage(proto.Usage{TotalTokens: used, ContextWindow: int64(jsonNumber(update, "size"))})
		}
	}
	peer, err = startJSONLPeer(ctx, spec, func(msg map[string]any) {
		if id := msg["id"]; id != nil && jsonString(msg, "method") == "session/request_permission" {
			params := jsonMap(msg, "params")
			options, _ := params["options"].([]any)
			optionID := acpPermissionOption(options, turn.Permission)
			result := map[string]any{"outcome": "cancelled"}
			if optionID != "" {
				result = map[string]any{"outcome": "selected", "optionId": optionID}
			}
			if turn.Permission == proto.PermissionSupervised {
				sink.Notice("ACP requested permission; Supervised mode denied it.")
			}
			_ = peer.respond(id, result, nil)
			return
		}
		handle(msg)
	})
	if err != nil {
		return err
	}
	defer peer.close()
	if _, err := acpRequest(ctx, peer, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "T3Go", "version": "0.1.0"},
	}, 20*time.Second); err != nil {
		return err
	}
	var session map[string]any
	if turn.SessionID != "" {
		session, err = acpRequest(ctx, peer, "session/load", map[string]any{
			"sessionId": turn.SessionID, "cwd": turn.CWD, "mcpServers": []any{},
		}, 60*time.Second)
		if err != nil {
			session = nil
		}
	}
	if session == nil {
		session, err = acpRequest(ctx, peer, "session/new", map[string]any{
			"cwd": turn.CWD, "mcpServers": []any{},
		}, 30*time.Second)
		if err != nil {
			return err
		}
	}
	sessionID := jsonString(session, "sessionId")
	if sessionID == "" {
		sessionID = turn.SessionID
	}
	if sessionID != "" {
		sink.Session(sessionID)
	}
	if turn.Model != "" && turn.Model != "default" {
		modelOption := acpModelOption(session)
		if modelOption == "" {
			if !containsACPModel(session, turn.Model) {
				sink.Notice("ACP provider did not advertise selectable model " + turn.Model + "; using its default.")
			}
		} else {
			if _, err := acpRequest(ctx, peer, "session/set_config_option", map[string]any{
				"sessionId": sessionID, "configId": modelOption, "value": turn.Model,
			}, 20*time.Second); err != nil {
				return err
			}
		}
	}
	readyMu.Lock()
	ready = true
	readyMu.Unlock()
	result, err := acpRequest(ctx, peer, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": turn.Prompt}},
	}, 30*time.Minute)
	if err != nil {
		return err
	}
	if reason := jsonString(result, "stopReason"); reason == "cancelled" {
		return context.Canceled
	}
	return nil
}

func acpPermissionOption(options []any, permission proto.PermissionMode) string {
	var fallback, reject string
	for _, raw := range options {
		option := valueMap(raw)
		id := jsonString(option, "optionId")
		kind := strings.ToLower(jsonString(option, "kind"))
		if id == "" {
			continue
		}
		if fallback == "" && strings.Contains(kind, "allow") {
			fallback = id
		}
		if reject == "" && (strings.Contains(kind, "reject") || strings.Contains(kind, "deny")) {
			reject = id
		}
	}
	if permission == proto.PermissionFullAccess || permission == proto.PermissionAuto {
		if fallback != "" {
			return fallback
		}
	}
	if permission == proto.PermissionAcceptEdits && fallback != "" {
		return fallback
	}
	if reject != "" {
		return reject
	}
	return fallback
}

func acpModelOption(session map[string]any) string {
	options, _ := session["configOptions"].([]any)
	for _, raw := range options {
		option := valueMap(raw)
		id := strings.ToLower(jsonString(option, "id"))
		name := strings.ToLower(jsonString(option, "name"))
		if jsonString(option, "type") == "select" && (strings.Contains(id, "model") || strings.Contains(name, "model")) {
			return jsonString(option, "id")
		}
	}
	return ""
}

func containsACPModel(session map[string]any, model string) bool {
	for _, candidate := range acpModels(session, "") {
		if candidate.ID == model {
			return true
		}
	}
	return false
}

func acpContentText(raw any) string {
	content := valueMap(raw)
	if jsonString(content, "type") == "text" {
		return jsonString(content, "text")
	}
	return ""
}

func acpToolOutput(update map[string]any) string {
	if raw := update["rawOutput"]; raw != nil {
		return valueText(raw)
	}
	content, _ := update["content"].([]any)
	var parts []string
	for _, block := range content {
		if text := acpContentText(block); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return ""
}
