package workerloop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestEvaluateActivePlanStepSatisfiedFromStepJudge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"status":"satisfied","reason":"archive metadata collected","summary":"archive metadata verified"}`}},
			},
		})
	}))
	defer server.Close()

	packet := ctxpacket.WorkerPacket{
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			ActiveStep: "Inspect archive",
		},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "unzip -l secret.zip",
			ExitStatus:    "0",
			Assessment:    "success",
			OutputSummary: "stdout: treasure-note.txt",
		},
	}
	got := evaluateActivePlanStep(context.Background(), llmclient.Client{BaseURL: server.URL, Model: "test-model", HTTPClient: server.Client()}, nil, packet, "")
	if got.Status != StepSatisfied {
		t.Fatalf("status = %q, want %q", got.Status, StepSatisfied)
	}
	if got.Summary != "archive metadata verified" {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestEvaluateActivePlanStepStaysInProgressOnRecoverableMissingPath(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		PlanState: ctxpacket.PlanState{
			Mode:             "planned_execution",
			ActiveStep:       "Attempt recovery",
			ReplanConditions: []string{"wordlist missing"},
		},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "fcrackzip -D -p /usr/share/wordlists/rockyou.txt secret.zip",
			ExitStatus:    "1",
			Assessment:    "failed",
			OutputSummary: "stderr: /usr/share/wordlists/rockyou.txt: No such file or directory",
			Signals:       []string{"missing_path", "nonzero_exit"},
			FailureClass:  "command_failed",
		},
	}
	got := evaluateActivePlanStep(context.Background(), llmclient.Client{}, nil, packet, "step complete")
	if got.Status != StepInProgress {
		t.Fatalf("status = %q, want %q", got.Status, StepInProgress)
	}
}

func TestEvaluateActivePlanStepBlockedWhenReplanConditionMatchesEvidence(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		PlanState: ctxpacket.PlanState{
			Mode:             "planned_execution",
			ActiveStep:       "Attempt recovery",
			ReplanConditions: []string{"wordlist missing"},
		},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "fcrackzip -D -p /usr/share/wordlists/rockyou.txt secret.zip",
			ExitStatus:    "1",
			Assessment:    "failed",
			OutputSummary: "stderr: wordlist missing",
			Signals:       []string{"missing_path", "nonzero_exit"},
			FailureClass:  "command_failed",
		},
	}
	got := evaluateActivePlanStep(context.Background(), llmclient.Client{}, nil, packet, "step complete")
	if got.Status != StepBlocked {
		t.Fatalf("status = %q, want %q", got.Status, StepBlocked)
	}
}

func TestEvaluateActivePlanStepRemainsInProgressWithoutEvidence(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			ActiveStep: "Identify services",
		},
	}
	got := evaluateActivePlanStep(context.Background(), llmclient.Client{}, nil, packet, "done")
	if got.Status != StepInProgress {
		t.Fatalf("status = %q, want %q", got.Status, StepInProgress)
	}
}

func TestEvaluateActivePlanStepInterruptedResultStaysInProgressWithoutLLM(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			ActiveStep: "Run scan",
		},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "nmap -sV --top-ports 1000 192.168.50.1",
			ExitStatus:    "-1",
			Assessment:    "ambiguous",
			OutputSummary: "stdout: Starting Nmap 7.98",
			Signals:       []string{"execution_timeout"},
			FailureClass:  "execution_interrupted",
		},
	}

	got := evaluateActivePlanStep(context.Background(), llmclient.Client{}, nil, packet, "")
	if got.Status != StepInProgress {
		t.Fatalf("status = %q, want %q", got.Status, StepInProgress)
	}
	if got.Reason != "active step work was interrupted after execution started" {
		t.Fatalf("reason = %q", got.Reason)
	}
}
