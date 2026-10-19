package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Configuration is operator-owned. Server commands are launched directly,
// without a shell, in the current worker workspace.
type Configuration struct {
	Servers map[string]Server `json:"servers"`
}

type Server struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

func Load(path string) (Configuration, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Configuration{}, nil
	}
	if err != nil {
		return Configuration{}, err
	}
	if len(data) > 1<<20 {
		return Configuration{}, fmt.Errorf("MCP configuration is too large")
	}
	var config Configuration
	if err := json.Unmarshal(data, &config); err != nil {
		return Configuration{}, fmt.Errorf("parse MCP configuration: %w", err)
	}
	for name, server := range config.Servers {
		if strings.TrimSpace(name) != name || name == "" || strings.ContainsAny(name, "\r\n\x00") || strings.TrimSpace(server.Command) == "" {
			return Configuration{}, fmt.Errorf("MCP server %q needs a simple name and an executable command", name)
		}
	}
	return config, nil
}

func (c Configuration) Names() []string {
	names := make([]string, 0, len(c.Servers))
	for name := range c.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) HasServer(name string) bool {
	_, ok := r.config.Servers[name]
	return ok
}

func (r *Registry) LaunchSpec(name string) string {
	server, ok := r.config.Servers[name]
	if !ok {
		return ""
	}
	arguments, _ := json.Marshal(server.Args)
	return server.Command + " " + string(arguments)
}

type Registry struct {
	config    Configuration
	workspace string
	runCtx    context.Context
	sessions  map[string]*mcp.ClientSession
	stops     map[string]context.CancelFunc
	tools     map[string]map[string]*mcp.Tool
}

func New(config Configuration, workspace string, runCtx context.Context) *Registry {
	return &Registry{config: config, workspace: workspace, runCtx: runCtx, sessions: make(map[string]*mcp.ClientSession), stops: make(map[string]context.CancelFunc), tools: make(map[string]map[string]*mcp.Tool)}
}

func (r *Registry) Close() error {
	for _, stop := range r.stops {
		stop()
	}
	var first error
	for _, session := range r.sessions {
		if err := session.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (r *Registry) connect(ctx context.Context, name string) (*mcp.ClientSession, error) {
	server, ok := r.config.Servers[name]
	if !ok {
		return nil, fmt.Errorf("MCP server %q is not configured", name)
	}
	if session := r.sessions[name]; session != nil {
		return session, nil
	}
	processCtx, stop := context.WithCancel(r.runCtx)
	command := exec.CommandContext(processCtx, server.Command, server.Args...)
	command.Dir = r.workspace
	client := mcp.NewClient(&mcp.Implementation{Name: "birdhackbot", Version: "1"}, nil)
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, err := client.Connect(connectCtx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		stop()
		return nil, fmt.Errorf("connect MCP server %q: %w", name, err)
	}
	r.sessions[name] = session
	r.stops[name] = stop
	return session, nil
}

func (r *Registry) availableTools(ctx context.Context, name string) (map[string]*mcp.Tool, error) {
	if known := r.tools[name]; known != nil {
		return known, nil
	}
	session, err := r.connect(ctx, name)
	if err != nil {
		return nil, err
	}
	known := make(map[string]*mcp.Tool)
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list MCP tools on %q: %w", name, err)
		}
		if tool != nil && tool.Name != "" {
			known[tool.Name] = tool
		}
	}
	r.tools[name] = known
	return known, nil
}

// List returns one compact, deterministic page. Tool schemas are fetched only
// when the worker asks to describe a specific name.
func (r *Registry) List(ctx context.Context, server string, offset int) ([]byte, error) {
	tools, err := r.availableTools(ctx, server)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	if offset < 0 || offset > len(names) {
		return nil, fmt.Errorf("MCP tool offset %d is outside 0..%d", offset, len(names))
	}
	end := min(offset+30, len(names))
	page := make([]map[string]string, 0, end-offset)
	for _, name := range names[offset:end] {
		desc := []rune(strings.TrimSpace(tools[name].Description))
		if len(desc) > 200 {
			desc = append(desc[:200], []rune("…")...)
		}
		page = append(page, map[string]string{"name": name, "description": string(desc)})
	}
	return json.Marshal(struct {
		Server     string              `json:"server"`
		Total      int                 `json:"total"`
		Offset     int                 `json:"offset"`
		NextOffset *int                `json:"next_offset,omitempty"`
		Tools      []map[string]string `json:"tools"`
	}{server, len(names), offset, func() *int {
		if end < len(names) {
			return &end
		}
		return nil
	}(), page})
}

func (r *Registry) Describe(ctx context.Context, server, tool string) ([]byte, error) {
	tools, err := r.availableTools(ctx, server)
	if err != nil {
		return nil, err
	}
	definition := tools[tool]
	if definition == nil {
		return nil, fmt.Errorf("MCP tool %q is not advertised by %q", tool, server)
	}
	return json.Marshal(definition)
}

func (r *Registry) Call(ctx context.Context, server, tool string, arguments json.RawMessage) ([]byte, error) {
	tools, err := r.availableTools(ctx, server)
	if err != nil {
		return nil, err
	}
	if tools[tool] == nil {
		return nil, fmt.Errorf("MCP tool %q is not advertised by %q", tool, server)
	}
	var args map[string]any
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return nil, fmt.Errorf("decode MCP tool arguments: %w", err)
		}
	}
	result, err := r.sessions[server].CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, fmt.Errorf("call MCP tool %q on %q: %w", tool, server, err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("record MCP result: %w", err)
	}
	if result.IsError {
		return data, fmt.Errorf("MCP tool %q reported an error", tool)
	}
	return data, nil
}

func DefaultConfigPath(workspace string) string {
	root := workspace
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return filepath.Join(root, "config", "mcp-servers.local.json")
		}
		parent := filepath.Dir(root)
		if parent == root {
			return ""
		}
		root = parent
	}
}
