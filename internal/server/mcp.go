package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tk/t3go/internal/proto"
)

type mcpServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
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
	servers := map[string]mcpServer{}
	loadMCPFile(filepath.Join(root, ".mcp.json"), servers)
	loadMCPFile(filepath.Join(root, ".claude", "settings.json"), servers)
	loadMCPFile(filepath.Join(root, ".claude", "settings.local.json"), servers)
	home, _ := os.UserHomeDir()
	if home != "" {
		loadMCPFile(filepath.Join(home, ".claude.json"), servers)
	}
	var names []string
	for name, server := range servers {
		if strings.TrimSpace(server.Command) != "" {
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
				commands = append(commands, proto.AgentCommand{
					Name:        "mcp__" + sanitizeMCP(name) + "__" + sanitizeMCP(prompt.Name),
					Description: prompt.Description,
					Kind:        "prompt", Source: "mcp",
				})
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

type mcpPrompt struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type mcpResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func queryMCPPrompts(root string, server mcpServer) ([]mcpPrompt, error) {
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

var mcpName = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitizeMCP(name string) string {
	return strings.Trim(mcpName.ReplaceAllString(name, "_"), "_")
}
