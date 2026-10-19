package mcptools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFixtureServer(t *testing.T) {
	if os.Getenv("BHB_MCP_FIXTURE") != "1" {
		return
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	type input struct {
		Text string `json:"text"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "Return the supplied text"}, func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Text}}}, nil, nil
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		os.Exit(2)
	}
}

func TestStdioDiscoveryAndCall(t *testing.T) {
	t.Setenv("BHB_MCP_FIXTURE", "1")
	r := New(Configuration{Servers: map[string]Server{"fixture": {Command: os.Args[0], Args: []string{"-test.run=^TestFixtureServer$"}}}}, t.TempDir(), context.Background())
	defer r.Close()
	ctx := context.Background()
	list, err := r.List(ctx, "fixture", 0)
	if err != nil || !strings.Contains(string(list), `"echo"`) {
		t.Fatalf("list: %s %v", list, err)
	}
	description, err := r.Describe(ctx, "fixture", "echo")
	if err != nil || !strings.Contains(string(description), `"text"`) {
		t.Fatalf("describe: %s %v", description, err)
	}
	result, err := r.Call(ctx, "fixture", "echo", json.RawMessage(`{"text":"observed"}`))
	if err != nil || !strings.Contains(string(result), "observed") {
		t.Fatalf("call: %s %v", result, err)
	}
	if _, err := r.Call(ctx, "fixture", "unadvertised", nil); err == nil {
		t.Fatal("unadvertised tool was callable")
	}
}
