package context

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestValidatePacketOK(t *testing.T) {
	packet := WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "inspect target", ReportingRequirement: "owasp"},
		CurrentStep:       Step{Objective: "inspect target"},
		RecentConversation: []string{
			"User: inspect target",
		},
		RunningSummary: "starting",
	}
	report := ValidatePacket(packet)
	if got := report.HighestSeverity(); got != ValidationInfo {
		t.Fatalf("HighestSeverity() = %q, want %q", got, ValidationInfo)
	}
	if got := report.Summary(); got != "ok" {
		t.Fatalf("Summary() = %q, want %q", got, "ok")
	}
}

func TestValidatePacketFatalForMissingGoalAndBehavior(t *testing.T) {
	packet := WorkerPacket{
		CurrentStep: Step{Objective: "x"},
	}
	report := ValidatePacket(packet)
	if !report.IsFatal() {
		t.Fatalf("IsFatal() = false, want true")
	}
	if got := report.HighestSeverity(); got != ValidationFatal {
		t.Fatalf("HighestSeverity() = %q, want %q", got, ValidationFatal)
	}
}

func TestValidatePacketWarnsOnMissingSummary(t *testing.T) {
	packet := WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "inspect target", ReportingRequirement: "owasp"},
		CurrentStep:       Step{Objective: "inspect target"},
	}
	report := ValidatePacket(packet)
	if got := report.HighestSeverity(); got != ValidationWarn {
		t.Fatalf("HighestSeverity() = %q, want %q", got, ValidationWarn)
	}
}

func TestValidatePacketErrorsOnRecentConversationOverflow(t *testing.T) {
	packet := WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "inspect target", ReportingRequirement: "owasp"},
		CurrentStep:       Step{Objective: "inspect target"},
		RunningSummary:    "summary",
	}
	for i := 0; i < recentConversationTurnLimit+1; i++ {
		packet.RecentConversation = append(packet.RecentConversation, "User: "+strings.Repeat("x", 10))
	}
	report := ValidatePacket(packet)
	if got := report.HighestSeverity(); got != ValidationError {
		t.Fatalf("HighestSeverity() = %q, want %q", got, ValidationError)
	}
}

func TestValidatePacketErrorsOnDoneWithMissingFact(t *testing.T) {
	packet := WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "inspect target", ReportingRequirement: "owasp"},
		CurrentStep:       Step{Objective: "inspect target"},
		RunningSummary:    "done",
		TaskRuntime: TaskRuntime{
			State:         "done",
			CurrentTarget: "secret.zip",
			MissingFact:   "next evidence needed about secret.zip",
		},
	}
	report := ValidatePacket(packet)
	if got := report.HighestSeverity(); got != ValidationError {
		t.Fatalf("HighestSeverity() = %q, want %q", got, ValidationError)
	}
}

func TestValidatePacketErrorsOnRuntimeArtifactTarget(t *testing.T) {
	packet := WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "inspect target", ReportingRequirement: "owasp"},
		CurrentStep:       Step{Objective: "inspect target"},
		RunningSummary:    "running",
		TaskRuntime: TaskRuntime{
			State:         "running",
			CurrentTarget: "/tmp/session-1/logs/cmd-20260322.log",
			MissingFact:   "next evidence needed",
		},
	}
	report := ValidatePacket(packet)
	if got := report.HighestSeverity(); got != ValidationError {
		t.Fatalf("HighestSeverity() = %q, want %q", got, ValidationError)
	}
}

func TestValidatePacketErrorsOnPendingWithoutAction(t *testing.T) {
	packet := WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "inspect target", ReportingRequirement: "owasp"},
		CurrentStep:       Step{Objective: "inspect target"},
		RunningSummary:    "running",
		OperatorState: OperatorState{
			PendingExec: "/bin/sh -lc \"pwd\"",
		},
	}
	report := ValidatePacket(packet)
	if got := report.HighestSeverity(); got != ValidationError {
		t.Fatalf("HighestSeverity() = %q, want %q", got, ValidationError)
	}
}

func TestValidatePacketErrorsOnPartialLatestExecutionResult(t *testing.T) {
	packet := WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "rules", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "inspect target", ReportingRequirement: "owasp"},
		CurrentStep:       Step{Objective: "inspect target"},
		RunningSummary:    "running",
		LatestExecutionResult: ExecutionResult{
			Action: "pwd",
		},
	}
	report := ValidatePacket(packet)
	if got := report.HighestSeverity(); got != ValidationError {
		t.Fatalf("HighestSeverity() = %q, want %q", got, ValidationError)
	}
}
