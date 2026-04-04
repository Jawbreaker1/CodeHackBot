package workerloop

import (
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestNormalizePlannedAuthorizationScopeAdvancesPrivateIPStep(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		TaskRuntime: ctxpacket.TaskRuntime{
			CurrentTarget: "192.168.50.1",
		},
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			ActiveStep: "Verify target is within authorized scope (internal network)",
			Steps: []string{
				"Verify target is within authorized scope (internal network)",
				"Execute port scan to identify open ports on target",
			},
		},
		CurrentStep: ctxpacket.Step{
			Objective: "Verify target is within authorized scope (internal network)",
		},
	}

	got := normalizePlannedAuthorizationScope(packet)
	if got.PlanState.ActiveStep != "Execute port scan to identify open ports on target" {
		t.Fatalf("active step = %q", got.PlanState.ActiveStep)
	}
	if got.CurrentStep.Objective != "Execute port scan to identify open ports on target" {
		t.Fatalf("current objective = %q", got.CurrentStep.Objective)
	}
}

func TestIsAuthorizationScopeStep(t *testing.T) {
	tests := []struct {
		step string
		want bool
	}{
		{step: "Verify target is within authorized scope (internal network)", want: true},
		{step: "Check scope authorization for target 192.168.50.1", want: true},
		{step: "Confirm in-scope status for target", want: true},
		{step: "Execute port scan to identify open ports on target", want: false},
		{step: "Attempt password recovery using appropriate cracking tools", want: false},
	}
	for _, tc := range tests {
		if got := isAuthorizationScopeStep(tc.step); got != tc.want {
			t.Fatalf("isAuthorizationScopeStep(%q) = %v, want %v", tc.step, got, tc.want)
		}
	}
}

func TestNormalizePlannedAuthorizationScopeDoesNotAdvancePublicTarget(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		TaskRuntime: ctxpacket.TaskRuntime{
			CurrentTarget: "8.8.8.8",
		},
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			ActiveStep: "Verify scope authorization for target 8.8.8.8",
			Steps: []string{
				"Verify scope authorization for target 8.8.8.8",
				"Execute port scan to identify open ports on target",
			},
		},
		CurrentStep: ctxpacket.Step{
			Objective: "Verify scope authorization for target 8.8.8.8",
		},
	}

	got := normalizePlannedAuthorizationScope(packet)
	if got.PlanState.ActiveStep != packet.PlanState.ActiveStep {
		t.Fatalf("active step changed unexpectedly to %q", got.PlanState.ActiveStep)
	}
}

func TestNormalizePlannedAuthorizationScopeAdvancesLocalArtifactStep(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		TaskRuntime: ctxpacket.TaskRuntime{
			CurrentTarget: "/home/johan/birdhackbot/CodeHackBot/secret.zip",
		},
		PlanState: ctxpacket.PlanState{
			Mode:       "planned_execution",
			ActiveStep: "Verify scope authorization for local artifact",
			Steps: []string{
				"Verify scope authorization for local artifact",
				"Examine secret.zip metadata and encryption details",
			},
		},
		CurrentStep: ctxpacket.Step{
			Objective: "Verify scope authorization for local artifact",
		},
	}

	got := normalizePlannedAuthorizationScope(packet)
	if got.PlanState.ActiveStep != "Examine secret.zip metadata and encryption details" {
		t.Fatalf("active step = %q", got.PlanState.ActiveStep)
	}
}

func TestShouldUseWorkerPlannerHonorsModeHint(t *testing.T) {
	packet := ctxpacket.WorkerPacket{
		SessionFoundation: ctxpacket.WorkerPacket{}.SessionFoundation,
		OperatorState: ctxpacket.OperatorState{
			ModeHint: "planned_execution",
		},
	}
	packet.SessionFoundation.Goal = "Who are you?"
	if !shouldUseWorkerPlanner(packet) {
		t.Fatalf("shouldUseWorkerPlanner() = false, want true for planned_execution hint")
	}

	packet.OperatorState.ModeHint = "direct_execution"
	packet.SessionFoundation.Goal = "Extract secret.zip and find the password"
	if shouldUseWorkerPlanner(packet) {
		t.Fatalf("shouldUseWorkerPlanner() = true, want false for direct_execution hint")
	}
}
