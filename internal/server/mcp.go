package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/tk/t3go/internal/proto"
)

type mcpServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type mcpConfigFile struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
	Projects   map[string]struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	} `json:"projects"`
}

// discoverMCPPrompts asks the workspace's local MCP servers for their
// prompts. Remote servers and Codex's TOML config are not read.
func discoverMCPPrompts(root string) []proto.AgentCommand {
	home, _ := os.UserHomeDir()
	return discoverMCPPromptsAt(root, home)
}

func discoverMCPPromptsAt(root, home string) []proto.AgentCommand {
	servers := map[string]mcpServer{}
	loadMCPFile(filepath.Join(root, ".mcp.json"), servers)
	loadMCPFile(filepath.Join(root, ".claude", "settings.json"), servers)
	loadMCPFile(filepath.Join(root, ".claude", "settings.local.json"), servers)
	if home != "" {
		loadMCPFile(filepath.Join(home, ".claude.json"), servers)
		loadCodexMCP(filepath.Join(home, ".codex", "config.toml"), servers)
	}
	var names []string
	for name, server := range servers {
		if strings.TrimSpace(server.Command) != "" || strings.TrimSpace(server.URL) != "" {
			names = append(names, name)
		}
		if len(names) >= 8 {
			break
		}
	}
	var mu sync.Mutex
	var commands []proto.AgentCommand
	var wait sync.WaitGroup
	for _, name := range names {
		server := servers[name]
		name, server := name, server
		wait.Add(1)
		go func() {
			defer wait.Done()
			prompts, err := queryMCPPrompts(root, server)
			if err != nil {
				return
			}
			mu.Lock()
			for _, prompt := range prompts {
				command := proto.AgentCommand{
					Name:        "mcp__" + sanitizeMCP(name) + "__" + sanitizeMCP(prompt.Name),
					Description: prompt.Description,
					Kind:        "prompt", Source: "mcp",
				}
				for _, arg := range prompt.Arguments {
					command.Arguments = append(command.Arguments, proto.AgentCommandArgument{
						Name: arg.Name, Description: arg.Description, Required: arg.Required,
					})
				}
				commands = append(commands, command)
			}
			mu.Unlock()
		}()
	}
	wait.Wait()
	return commands
}

func loadMCPFile(path string, servers map[string]mcpServer) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var file mcpConfigFile
	if json.Unmarshal(raw, &file) != nil {
		return
	}
	for name, server := range file.MCPServers {
		if _, ok := servers[name]; !ok {
			servers[name] = server
		}
	}
	for _, project := range file.Projects {
		for name, server := range project.MCPServers {
			if _, ok := servers[name]; !ok {
				servers[name] = server
			}
		}
	}
}

// loadCodexMCP reads Codex's TOML mcp_servers table.
func loadCodexMCP(path string, servers map[string]mcpServer) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var config struct {
		Servers map[string]struct {
			Command string            `toml:"command"`
			Args    []string          `toml:"args"`
			Env     map[string]string `toml:"env"`
		} `toml:"mcp_servers"`
	}
	if toml.Unmarshal(raw, &config) != nil {
		return
	}
	for name, server := range config.Servers {
		if _, ok := servers[name]; ok {
			continue
		}
		servers[name] = mcpServer{Command: server.Command, Args: server.Args, Env: server.Env}
	}
}

type mcpPrompt struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Arguments   []mcpPromptArgument `json:"arguments"`
}

type mcpPromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type mcpResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func queryMCPPrompts(root string, server mcpServer) ([]mcpPrompt, error) {
	if strings.EqualFold(server.Type, "sse") {
		return queryMCPSSE(server)
	}
	if strings.TrimSpace(server.URL) != "" {
		return queryMCPHTTP(server)
	}
	return queryMCPStdio(root, server)
}

func queryMCPStdio(root string, server mcpServer) ([]mcpPrompt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	cmd.Dir = root
	env := os.Environ()
	for key, value := range server.Env {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	encoder := json.NewEncoder(stdin)
	decoder := json.NewDecoder(stdout)
	send := func(value any) error { return encoder.Encode(value) }
	read := func(id int) (mcpResponse, error) {
		for {
			var response mcpResponse
			if err := decoder.Decode(&response); err != nil {
				return response, err
			}
			if response.ID == id {
				return response, nil
			}
		}
	}
	if err := send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "t3go", "version": "0.1"},
	}}); err != nil {
		return nil, err
	}
	if _, err := read(1); err != nil {
		return nil, err
	}
	_ = send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}})
	if err := send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "prompts/list", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	response, err := read(2)
	if err != nil {
		return nil, err
	}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		return nil, errors.New("mcp prompts/list failed")
	}
	var result struct {
		Prompts []mcpPrompt `json:"prompts"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, err
	}
	return result.Prompts, nil
}

// queryMCPHTTP reads prompts from a streamable HTTP MCP server. Both
// plain JSON and SSE responses to each request are supported; legacy
// GET /sse transports are not.
func queryMCPHTTP(server mcpServer) ([]mcpPrompt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second}
	session := ""
	post := func(value any) ([]mcpResponse, error) {
		body, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("MCP-Protocol-Version", "2024-11-05")
		if session != "" {
			request.Header.Set("Mcp-Session-Id", session)
		}
		for key, value := range server.Headers {
			request.Header.Set(key, value)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if id := response.Header.Get("Mcp-Session-Id"); id != "" {
			session = id
		}
		if response.StatusCode >= 400 {
			return nil, errors.New("mcp http status " + response.Status)
		}
		contentType := response.Header.Get("Content-Type")
		if strings.Contains(contentType, "text/event-stream") {
			var out []mcpResponse
			scanner := bufio.NewScanner(response.Body)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				var message mcpResponse
				if json.Unmarshal([]byte(data), &message) == nil {
					out = append(out, message)
				}
			}
			return out, scanner.Err()
		}
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil, nil
		}
		var message mcpResponse
		if err := json.Unmarshal(raw, &message); err != nil {
			return nil, err
		}
		return []mcpResponse{message}, nil
	}
	find := func(messages []mcpResponse, id int) (mcpResponse, bool) {
		for _, message := range messages {
			if message.ID == id {
				return message, true
			}
		}
		return mcpResponse{}, false
	}
	if _, err := post(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "t3go", "version": "0.1"},
	}}); err != nil {
		return nil, err
	}
	_, _ = post(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}})
	messages, err := post(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "prompts/list", "params": map[string]any{}})
	if err != nil {
		return nil, err
	}
	response, ok := find(messages, 2)
	if !ok || len(response.Error) > 0 && string(response.Error) != "null" {
		return nil, errors.New("mcp prompts/list failed")
	}
	var result struct {
		Prompts []mcpPrompt `json:"prompts"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, err
	}
	return result.Prompts, nil
}

// queryMCPSSE reads prompts from a legacy HTTP+SSE MCP server: a GET
// stream announces the messages endpoint, and responses arrive on it.
func queryMCPSSE(server mcpServer) ([]mcpPrompt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/event-stream")
	for key, value := range server.Headers {
		request.Header.Set(key, value)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, errors.New("mcp sse status " + response.Status)
	}
	endpoint := make(chan string, 1)
	messages := make(chan mcpResponse, 32)
	go func() {
		scanner := bufio.NewScanner(response.Body)
		event := ""
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if event == "endpoint" || strings.HasPrefix(data, "/") && strings.Contains(data, "sessionId=") {
					select {
					case endpoint <- data:
					case <-ctx.Done():
						return
					}
					event = ""
					continue
				}
				var message mcpResponse
				if json.Unmarshal([]byte(data), &message) == nil {
					select {
					case messages <- message:
					case <-ctx.Done():
						return
					}
				}
			case line == "":
				event = ""
			}
		}
	}()
	var endpointPath string
	select {
	case endpointPath = <-endpoint:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	base, err := url.Parse(server.URL)
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(endpointPath)
	if err != nil {
		return nil, err
	}
	endpointURL := base.ResolveReference(target).String()
	post := func(value any) error {
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		for key, value := range server.Headers {
			req.Header.Set(key, value)
		}
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return errors.New("mcp sse post status " + resp.Status)
		}
		return nil
	}
	read := func(id int) (mcpResponse, error) {
		for {
			select {
			case message := <-messages:
				if message.ID == id {
					return message, nil
				}
			case <-ctx.Done():
				return mcpResponse{}, ctx.Err()
			}
		}
	}
	if err := post(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "t3go", "version": "0.1"},
	}}); err != nil {
		return nil, err
	}
	if _, err := read(1); err != nil {
		return nil, err
	}
	_ = post(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}})
	if err := post(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "prompts/list", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	responseMessage, err := read(2)
	if err != nil {
		return nil, err
	}
	if len(responseMessage.Error) > 0 && string(responseMessage.Error) != "null" {
		return nil, errors.New("mcp prompts/list failed")
	}
	var result struct {
		Prompts []mcpPrompt `json:"prompts"`
	}
	if err := json.Unmarshal(responseMessage.Result, &result); err != nil {
		return nil, err
	}
	return result.Prompts, nil
}

var mcpName = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitizeMCP(name string) string {
	return strings.Trim(mcpName.ReplaceAllString(name, "_"), "_")
}
