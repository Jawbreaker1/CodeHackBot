package workerloop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkerMCPFixtureServer(t *testing.T) {
	if os.Getenv("BHB_WORKER_MCP_FIXTURE") != "1" {
		return
	}
	_ = os.WriteFile(os.Getenv("BHB_WORKER_MCP_MARKER"), []byte("started"), 0600)
	server := mcp.NewServer(&mcp.Implementation{Name: "worker-fixture", Version: "1"}, nil)
	type input struct {
		Text string `json:"text"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "Echo text"}, func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Text}}}, nil, nil
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		os.Exit(2)
	}
}

func TestMCPActionUsesApprovalAndEvidence(t *testing.T) {
	loop, packet, _ := fixtureWorker(t, 2)
	packet.Budget.Used = 1
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv("BHB_WORKER_MCP_FIXTURE", "1")
	t.Setenv("BHB_WORKER_MCP_MARKER", marker)
	config := mcptools.Configuration{Servers: map[string]mcptools.Server{"fixture": {Command: os.Args[0], Args: []string{"-test.run=^TestWorkerMCPFixtureServer$"}}}}
	loop.MCP = mcptools.New(config, packet.OperatorState.WorkingDir, context.Background())
	defer loop.MCP.Close()
	response := Response{Type: "mcp_call", MCPServer: "fixture", MCPTool: "echo", MCPArguments: json.RawMessage(`{"text":"evidence"}`), Summary: "Echo a fixture value", Target: "local fixture", Impact: "Reads and returns the fixture value", Risk: "low"}
	loop.Approver = approval.StaticApprover{Decision: approval.DecisionDeny}
	_, err := loop.execute(context.Background(), &packet, response)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("denial: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("MCP server started before approval: %v", err)
	}
	loop.Approver = approval.StaticApprover{Decision: approval.DecisionApproveOnce}
	packet.TaskRuntime.State = "running"
	executed, err := loop.execute(context.Background(), &packet, response)
	if err != nil || !executed || !strings.Contains(packet.LatestExecutionResult.OutputEvidence, "evidence") {
		t.Fatalf("approved call: executed=%v err=%v result=%+v", executed, err, packet.LatestExecutionResult)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("server did not start after approval: %v", err)
	}
	for _, path := range append(packet.LatestExecutionResult.LogRefs, packet.LatestExecutionResult.ArtifactRefs...) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing evidence %s: %v", path, err)
		}
	}
}

func TestWorkerDiscoversAndCallsConfiguredMCPTool(t *testing.T) {
	t.Setenv("BHB_WORKER_MCP_FIXTURE", "1")
	t.Setenv("BHB_WORKER_MCP_MARKER", filepath.Join(t.TempDir(), "started"))
	loop, packet, _ := fixtureWorker(t, 4,
		`{"type":"mcp_list","mcp_server":"fixture"}`,
		`{"type":"mcp_describe","mcp_server":"fixture","mcp_tool":"echo"}`,
		`{"type":"mcp_call","mcp_server":"fixture","mcp_tool":"echo","mcp_arguments":{"text":"observed evidence"},"summary":"Read the fixture value","target":"local fixture","impact":"Returns a synthetic value","risk":"low"}`,
		`{"type":"step_complete","summary":"The fixture returned observed evidence."}`,
		completeEval,
	)
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	configBytes, _ := json.Marshal(mcptools.Configuration{Servers: map[string]mcptools.Server{"fixture": {Command: os.Args[0], Args: []string{"-test.run=^TestWorkerMCPFixtureServer$"}}}})
	if err := os.WriteFile(configPath, configBytes, 0600); err != nil {
		t.Fatal(err)
	}
	loop.MCPConfigPath = configPath
	out, err := loop.Run(context.Background(), packet, 4)
	if err != nil || out.Packet.TaskRuntime.State != "done" || !strings.Contains(out.Summary, "observed evidence") {
		t.Fatalf("MCP worker run: state=%s summary=%q err=%v", out.Packet.TaskRuntime.State, out.Summary, err)
	}
	if !strings.Contains(strings.Join(out.Packet.CapabilityInputs, "\n"), "Configured MCP servers: fixture") {
		t.Fatal("worker did not receive configured MCP server name")
	}
}
