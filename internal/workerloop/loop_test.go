package workerloop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextinspect"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestLoopStepComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"type":"step_complete","summary":"done"}`}},
			},
		})
	}))
	defer server.Close()

	loop := Loop{
		LLM:      llmclient.Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()},
		Executor: execx.Executor{LogDir: t.TempDir()},
		Approver: approval.StaticApprover{Decision: approval.DecisionApproveSession},
	}
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "test", ReportingRequirement: "owasp"},
	}
	outcome, err := loop.Run(context.Background(), packet, 1)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if outcome.Summary != "done" {
		t.Fatalf("summary = %q", outcome.Summary)
	}
}

func TestLoopWritesInspectionSnapshots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"type":"step_complete","summary":"done"}`}},
			},
		})
	}))
	defer server.Close()

	inspectDir := t.TempDir()
	loop := Loop{
		LLM:       llmclient.Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()},
		Executor:  execx.Executor{LogDir: t.TempDir()},
		Approver:  approval.StaticApprover{Decision: approval.DecisionApproveSession},
		Inspector: contextinspect.Recorder{Dir: inspectDir},
	}
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "test", ReportingRequirement: "owasp"},
	}
	if _, err := loop.Run(context.Background(), packet, 1); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(inspectDir, "step-001-pre-llm.txt"),
		filepath.Join(inspectDir, "step-001-step-complete.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected snapshot %s: %v", path, err)
		}
	}
}

func TestLoopApprovalDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"type":"action","command":"pwd","use_shell":false}`}},
			},
		})
	}))
	defer server.Close()

	loop := Loop{
		LLM:      llmclient.Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()},
		Executor: execx.Executor{LogDir: t.TempDir()},
		Approver: approval.StaticApprover{Decision: approval.DecisionDeny},
	}
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame:      behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation:  session.Foundation{Goal: "test", ReportingRequirement: "owasp"},
		RunningSummary:     "start",
		RecentConversation: []string{"user: test"},
	}
	_, err := loop.Run(context.Background(), packet, 1)
	if err == nil || !strings.Contains(err.Error(), "execution denied by user") {
		t.Fatalf("Run() error = %v, want denial", err)
	}
}

func TestUpdateRelevantRecentResults(t *testing.T) {
	current := []ctxpacket.ExecutionResult{
		{Action: "second", ExitStatus: "0"},
		{Action: "third", ExitStatus: "1"},
		{Action: "fourth", ExitStatus: "0"},
	}
	got := updateRelevantRecentResults(current, ctxpacket.ExecutionResult{Action: "first", ExitStatus: "0"})
	if len(got) != 3 {
		t.Fatalf("len(got) = %d", len(got))
	}
	if got[0].Action != "first" || got[1].Action != "second" || got[2].Action != "third" {
		t.Fatalf("unexpected ordering: %#v", got)
	}
}

func TestBuildRunningSummary(t *testing.T) {
	summary := buildRunningSummary(
		"inspect archive",
		ctxpacket.ExecutionResult{
			Action:        "file ./secret.zip",
			ExitStatus:    "0",
			OutputSummary: "stdout: ./secret.zip: ASCII text",
		},
		[]ctxpacket.ExecutionResult{
			{Action: "ls -la ./secret.zip", ExitStatus: "0"},
		},
	)
	for _, want := range []string{
		"Objective: inspect archive.",
		`Latest result: "file ./secret.zip" exited with 0.`,
		"Key output: stdout: ./secret.zip: ASCII text.",
		"Recent prior results retained: 1.",
		"Current status: in progress.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q in %q", want, summary)
		}
	}
}
