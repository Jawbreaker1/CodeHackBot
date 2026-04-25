package workerloop

import (
	"context"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestEvaluateDirectExecutionSatisfiedFromStructuredSuccessWithoutLLM(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "find /home/johan/birdhackbot/CodeHackBot -name \"*.zip\"",
			ExitStatus:    "0",
			OutputSummary: "stdout: /home/johan/birdhackbot/CodeHackBot/secret.zip",
			Assessment:    "success",
		},
	}

	got := evaluateDirectExecution(context.Background(), llmclient.Client{}, nil, packet)
	if got.Status != StepSatisfied {
		t.Fatalf("status = %q, want %q", got.Status, StepSatisfied)
	}
	if got.Reason != "latest successful execution already answers the direct request" {
		t.Fatalf("reason = %q", got.Reason)
	}
	if got.Summary != "" {
		t.Fatalf("summary = %q, want empty human-facing summary", got.Summary)
	}
}

func TestEvaluateDirectExecutionUsesLLMWhenSuccessIsNotClear(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "touch jokes.txt",
			ExitStatus:    "0",
			OutputSummary: "(none)",
			Assessment:    "ambiguous",
			Signals:       []string{"no_effect"},
		},
	}

	got := evaluateDirectExecution(context.Background(), llmclient.Client{}, nil, packet)
	if got.Status != StepInProgress {
		t.Fatalf("status = %q, want %q", got.Status, StepInProgress)
	}
}

func TestEvaluateDirectExecutionBlockedFromStructuredIncorrectPasswordWithoutLLM(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "unzip -o secret.zip -d ./extracted/",
			ExitStatus:    "5",
			OutputSummary: "stdout: unable to get password",
			Assessment:    "failed",
			Signals:       []string{"nonzero_exit", "incorrect_password"},
			FailureClass:  "command_failed",
		},
	}

	got := evaluateDirectExecution(context.Background(), llmclient.Client{}, nil, packet)
	if got.Status != StepBlocked {
		t.Fatalf("status = %q, want %q", got.Status, StepBlocked)
	}
	if got.Reason != "latest result indicates the direct request needs missing credentials" {
		t.Fatalf("reason = %q", got.Reason)
	}
}
