package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestEvaluateRunTerminalOutcomeFailsOnUnverifiedHighImpactClaim(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-terminal-truth-gate-fail"
	manager := orchestrator.NewManager(base)
	planPath := filepath.Join(base, "plan.json")
	writePlanFile(t, planPath, orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "verify claim",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"failed"},
				ExpectedArtifacts: []string{"log.txt"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget:            orchestrator.TaskBudget{MaxSteps: 2, MaxToolCalls: 2, MaxRuntime: time.Minute},
			},
		},
	})
	if _, err := manager.Start(planPath, runID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	now := time.Now().UTC()
	if err := manager.WriteLease(runID, orchestrator.TaskLease{
		TaskID:    "t1",
		LeaseID:   "lease-t1-1",
		WorkerID:  "worker-t1-a1",
		Status:    orchestrator.LeaseStatusCompleted,
		Attempt:   1,
		StartedAt: now,
		Deadline:  now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("WriteLease: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", orchestrator.EventTypeTaskFinding, map[string]any{
		"target":       "127.0.0.1",
		"finding_type": "web_vuln",
		"title":        "Potential RCE claim",
		"severity":     "high",
		"confidence":   "medium",
		"evidence":     []string{"response signature"},
		"source":       "test",
	}); err != nil {
		t.Fatalf("EmitEvent finding: %v", err)
	}
	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	outcome, detail, err := evaluateRunTerminalOutcome(manager, runID)
	if err != nil {
		t.Fatalf("evaluateRunTerminalOutcome: %v", err)
	}
	if outcome != runOutcomeFailure {
		t.Fatalf("expected failure outcome, got %s (%s)", outcome, detail)
	}
	if !strings.Contains(detail, "report_truth_gate_failed") {
		t.Fatalf("expected report truth gate failure detail, got %q", detail)
	}
}

func TestEvaluateRunTerminalOutcomePassesWithVerifiedFindingEvidence(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-terminal-truth-gate-pass"
	manager := orchestrator.NewManager(base)
	planPath := filepath.Join(base, "plan.json")
	writePlanFile(t, planPath, orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "verify claim",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"failed"},
				ExpectedArtifacts: []string{"scan.log"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget:            orchestrator.TaskBudget{MaxSteps: 2, MaxToolCalls: 2, MaxRuntime: time.Minute},
			},
		},
	})
	if _, err := manager.Start(planPath, runID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	now := time.Now().UTC()
	if err := manager.WriteLease(runID, orchestrator.TaskLease{
		TaskID:    "t1",
		LeaseID:   "lease-t1-1",
		WorkerID:  "worker-t1-a1",
		Status:    orchestrator.LeaseStatusCompleted,
		Attempt:   1,
		StartedAt: now,
		Deadline:  now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("WriteLease: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", orchestrator.EventTypeTaskArtifact, map[string]any{
		"type":  "log",
		"title": "scan log",
		"path":  filepath.ToSlash(filepath.Join("sessions", runID, "logs", "scan.log")),
	}); err != nil {
		t.Fatalf("EmitEvent artifact: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", orchestrator.EventTypeTaskFinding, map[string]any{
		"target":       "127.0.0.1",
		"finding_type": "open_port",
		"title":        "SSH open",
		"state":        orchestrator.FindingStateVerified,
		"severity":     "low",
		"confidence":   "high",
		"evidence":     []string{"22/tcp open"},
		"source":       "test",
	}); err != nil {
		t.Fatalf("EmitEvent finding: %v", err)
	}
	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	outcome, detail, err := evaluateRunTerminalOutcome(manager, runID)
	if err != nil {
		t.Fatalf("evaluateRunTerminalOutcome: %v", err)
	}
	if outcome != runOutcomeSuccess {
		t.Fatalf("expected success outcome, got %s (%s)", outcome, detail)
	}
	if detail != "all_tasks_completed" {
		t.Fatalf("unexpected success detail: %q", detail)
	}
}
