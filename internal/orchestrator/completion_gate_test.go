package orchestrator

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvaluateCompletionVerificationGateFailsWithoutCompletionContract(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-completion-gate-fail"
	manager := NewManager(base)
	planPath := filepath.Join(base, "plan.json")
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "collect output",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"failed"},
				ExpectedArtifacts: []string{"output.txt"},
				RiskLevel:         string(RiskReconReadonly),
				Budget:            TaskBudget{MaxSteps: 2, MaxToolCalls: 2, MaxRuntime: time.Minute},
			},
		},
	}
	if err := WriteJSONAtomic(planPath, plan); err != nil {
		t.Fatalf("WriteJSONAtomic plan: %v", err)
	}
	if _, err := manager.Start(planPath, runID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	now := time.Now().UTC()
	if err := manager.WriteLease(runID, TaskLease{
		TaskID:    "t1",
		LeaseID:   "lease-t1-1",
		WorkerID:  "worker-t1-a1",
		Status:    LeaseStatusCompleted,
		Attempt:   1,
		StartedAt: now,
		Deadline:  now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("WriteLease: %v", err)
	}

	summary, err := manager.EvaluateCompletionVerificationGate(runID)
	if err != nil {
		t.Fatalf("EvaluateCompletionVerificationGate: %v", err)
	}
	if summary.VerificationGate != "fail" {
		t.Fatalf("expected fail gate, got %q", summary.VerificationGate)
	}
	if summary.UnverifiedTasks != 1 {
		t.Fatalf("expected 1 unverified task, got %d", summary.UnverifiedTasks)
	}
	if !strings.Contains(summary.VerificationGateReason, "no task_completed evidence") {
		t.Fatalf("expected missing completion evidence reason, got %q", summary.VerificationGateReason)
	}
}

func TestEvaluateCompletionVerificationGatePassesWithSatisfiedContract(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-completion-gate-pass"
	manager := NewManager(base)
	planPath := filepath.Join(base, "plan.json")
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "collect output",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"failed"},
				ExpectedArtifacts: []string{"scan.log"},
				RiskLevel:         string(RiskReconReadonly),
				Budget:            TaskBudget{MaxSteps: 2, MaxToolCalls: 2, MaxRuntime: time.Minute},
			},
		},
	}
	if err := WriteJSONAtomic(planPath, plan); err != nil {
		t.Fatalf("WriteJSONAtomic plan: %v", err)
	}
	if _, err := manager.Start(planPath, runID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	now := time.Now().UTC()
	if err := manager.WriteLease(runID, TaskLease{
		TaskID:    "t1",
		LeaseID:   "lease-t1-1",
		WorkerID:  "worker-t1-a1",
		Status:    LeaseStatusCompleted,
		Attempt:   1,
		StartedAt: now,
		Deadline:  now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("WriteLease: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", EventTypeTaskCompleted, map[string]any{
		"attempt":   1,
		"worker_id": "worker-t1-a1",
		"log_path":  "scan.log",
		"completion_contract": map[string]any{
			"verification_status": "reported_by_worker",
			"required_artifacts":  []string{"scan.log"},
			"produced_artifacts":  []string{"scan.log"},
			"required_findings":   []string{"task_execution_result"},
			"produced_findings":   []string{"task_execution_result"},
		},
	}); err != nil {
		t.Fatalf("EmitEvent task_completed: %v", err)
	}

	summary, err := manager.EvaluateCompletionVerificationGate(runID)
	if err != nil {
		t.Fatalf("EvaluateCompletionVerificationGate: %v", err)
	}
	if summary.VerificationGate != "pass" {
		t.Fatalf("expected pass gate, got %q (%s)", summary.VerificationGate, summary.VerificationGateReason)
	}
	if summary.VerifiedCompletedTasks != 1 || summary.UnverifiedTasks != 0 {
		t.Fatalf("unexpected gate counts: verified=%d unverified=%d", summary.VerifiedCompletedTasks, summary.UnverifiedTasks)
	}
}
