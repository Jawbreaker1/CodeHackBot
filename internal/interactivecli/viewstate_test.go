package interactivecli

import (
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestBuildViewStateProjectsPacket(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{
			Goal:                 "Extract contents of secret.zip",
			ReportingRequirement: "owasp",
		},
		CurrentStep: ctxpacket.Step{
			Objective: "Attempt password recovery",
		},
		TaskRuntime: ctxpacket.TaskRuntime{
			State:         "running",
			CurrentTarget: "/tmp/secret.zip",
			MissingFact:   "password needed",
		},
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			WorkerGoal: "Recover the password and extract the archive",
			Summary:    "Inspect, recover, extract, report",
			Steps: []string{
				"Inspect metadata",
				"Attempt password recovery",
				"Extract contents",
			},
			ActiveStep: "Attempt password recovery",
		},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:        "fcrackzip -u -D ...",
			OutputSummary: "wordlist missing",
			Assessment:    "failed",
			Signals:       []string{"missing_path", "action_needs_revision"},
			FailureClass:  "pre_execution_review",
			LogRefs:       []string{"/tmp/cmd.log"},
		},
		OperatorState: ctxpacket.OperatorState{
			ScopeState:    "from_session_foundation",
			ApprovalState: "approved_session",
			Model:         "qwen3.5-27b",
			ContextUsage:  "12k tokens",
			WorkingDir:    "/work",
		},
	}

	got := BuildViewState(packet, "active")
	if got.SessionStatus != "active" {
		t.Fatalf("SessionStatus = %q", got.SessionStatus)
	}
	if got.Goal != "Extract contents of secret.zip" {
		t.Fatalf("Goal = %q", got.Goal)
	}
	if got.WorkerGoal != "Recover the password and extract the archive" {
		t.Fatalf("WorkerGoal = %q", got.WorkerGoal)
	}
	if got.CurrentStepState != StepVisualInProgress {
		t.Fatalf("CurrentStepState = %q", got.CurrentStepState)
	}
	if got.LatestActionReview != ReviewVisualRevise {
		t.Fatalf("LatestActionReview = %q", got.LatestActionReview)
	}
	if got.LatestStepEval != StepVisualInProgress {
		t.Fatalf("LatestStepEval = %q", got.LatestStepEval)
	}
	if got.LatestLog != "/tmp/cmd.log" {
		t.Fatalf("LatestLog = %q", got.LatestLog)
	}
	if len(got.PlanSteps) != 3 {
		t.Fatalf("len(PlanSteps) = %d", len(got.PlanSteps))
	}
	if got.PlanSteps[0].State != StepVisualDone {
		t.Fatalf("PlanSteps[0].State = %q", got.PlanSteps[0].State)
	}
	if got.PlanSteps[1].State != StepVisualInProgress {
		t.Fatalf("PlanSteps[1].State = %q", got.PlanSteps[1].State)
	}
	if got.PlanSteps[2].State != StepVisualPending {
		t.Fatalf("PlanSteps[2].State = %q", got.PlanSteps[2].State)
	}
}

func TestBuildViewStateMarksBlockedStep(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "router recon"},
		CurrentStep:       ctxpacket.Step{Objective: "Scan target"},
		TaskRuntime:       ctxpacket.TaskRuntime{State: "blocked"},
		PlanState: ctxpacket.PlanState{
			Steps:       []string{"Ping target", "Scan target"},
			ActiveStep:  "Scan target",
			BlockedStep: "Scan target",
		},
	}

	got := BuildViewState(packet, "stopped")
	if got.CurrentStepState != StepVisualBlocked {
		t.Fatalf("CurrentStepState = %q", got.CurrentStepState)
	}
	if got.PlanSteps[1].State != StepVisualBlocked {
		t.Fatalf("PlanSteps[1].State = %q", got.PlanSteps[1].State)
	}
	if got.LatestStepEval != StepVisualBlocked {
		t.Fatalf("LatestStepEval = %q", got.LatestStepEval)
	}
}

func TestBuildViewStateLeavesUnknownActionReviewWhenNoPreExecutionReview(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: session.Foundation{Goal: "router recon"},
		LatestExecutionResult: ctxpacket.ExecutionResult{
			Action:       "nmap -sV 192.168.50.1",
			FailureClass: "command_failed",
			Signals:      []string{"nonzero_exit"},
		},
	}

	got := BuildViewState(packet, "active")
	if got.LatestActionReview != ReviewVisualUnknown {
		t.Fatalf("LatestActionReview = %q", got.LatestActionReview)
	}
}
