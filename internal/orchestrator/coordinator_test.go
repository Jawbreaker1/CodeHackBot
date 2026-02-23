package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCoordinator_DependencyAndCompletion(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-1"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       time.Now().UTC(),
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  2,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
			task("t2", nil, 1),
			task("t3", []string{"t1"}, 1),
		},
	}
	scheduler, err := NewScheduler(plan, 2)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	waitCoordinatorDone(t, coord, 5*time.Second)

	for _, id := range []string{"t1", "t2", "t3"} {
		st, ok := scheduler.State(id)
		if !ok {
			t.Fatalf("missing task state: %s", id)
		}
		if st != TaskStateCompleted {
			t.Fatalf("expected completed state for %s, got %s", id, st)
		}
	}
}

func TestCoordinator_CompletionContractMissingArtifactsConvertsToFailure(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-completion-contract-fail"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       time.Now().UTC(),
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	var launchedWorkerID string
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 1, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		launchedWorkerID = workerID
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick launch: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return workers.CompletedCount() == 1
	})
	signalWorkerID := WorkerSignalID(launchedWorkerID)
	if err := manager.EmitEvent(runID, signalWorkerID, "t1", EventTypeTaskCompleted, map[string]any{
		"worker_id": launchedWorkerID,
		"completion_contract": map[string]any{
			"required_artifacts": []string{"artifact"},
			"produced_artifacts": []string{filepath.Join(base, "missing.log")},
			"required_findings":  []string{"task_execution_result"},
		},
	}); err != nil {
		t.Fatalf("EmitEvent task_completed: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick completion: %v", err)
	}

	st, _ := scheduler.State("t1")
	if st != TaskStateFailed {
		t.Fatalf("expected failed state after completion contract violation, got %s", st)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	failed := latestTaskFailedByReason(events, "missing_required_artifacts")
	if failed == nil {
		t.Fatalf("expected task_failed with missing_required_artifacts reason")
	}
	payload := map[string]any{}
	if len(failed.Payload) > 0 {
		_ = json.Unmarshal(failed.Payload, &payload)
	}
	if status := toString(payload["verification_status"]); status != "failed" {
		t.Fatalf("expected verification_status failed, got %q", status)
	}
	if len(sliceFromAny(payload["missing_artifacts"])) == 0 {
		t.Fatalf("expected missing_artifacts diagnostics in failure payload")
	}
}

func TestCoordinator_CompletionContractValidRemainsCompleted(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-completion-contract-success"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       time.Now().UTC(),
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	var launchedWorkerID string
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 1, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		launchedWorkerID = workerID
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick launch: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return workers.CompletedCount() == 1
	})
	artifactPath := filepath.Join(base, "artifact.log")
	if err := os.WriteFile(artifactPath, []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	signalWorkerID := WorkerSignalID(launchedWorkerID)
	if err := manager.EmitEvent(runID, signalWorkerID, "t1", EventTypeTaskArtifact, map[string]any{
		"path":  artifactPath,
		"title": "artifact",
		"type":  "command_log",
	}); err != nil {
		t.Fatalf("EmitEvent task_artifact: %v", err)
	}
	if err := manager.EmitEvent(runID, signalWorkerID, "t1", EventTypeTaskFinding, map[string]any{
		"target":       "192.168.50.0/24",
		"finding_type": "task_execution_result",
		"title":        "execution complete",
		"severity":     "info",
		"confidence":   "high",
		"evidence":     []string{artifactPath},
	}); err != nil {
		t.Fatalf("EmitEvent task_finding: %v", err)
	}
	if err := manager.EmitEvent(runID, signalWorkerID, "t1", EventTypeTaskCompleted, map[string]any{
		"worker_id": launchedWorkerID,
		"log_path":  artifactPath,
		"completion_contract": map[string]any{
			"required_artifacts": []string{"artifact"},
			"produced_artifacts": []string{artifactPath},
			"required_findings":  []string{"task_execution_result"},
		},
	}); err != nil {
		t.Fatalf("EmitEvent task_completed: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick completion: %v", err)
	}

	st, _ := scheduler.State("t1")
	if st != TaskStateCompleted {
		t.Fatalf("expected completed state, got %s", st)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if latestTaskFailedByReason(events, "missing_required_artifacts") != nil {
		t.Fatalf("did not expect missing_required_artifacts failure for valid completion contract")
	}
}

func TestCoordinator_RetryFailedTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-2"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       time.Now().UTC(),
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	scheduler.SetRetryBackoff([]time.Duration{20 * time.Millisecond, 40 * time.Millisecond})
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		if attempt == 1 {
			return helperWorkerSpec(t, workerID, "fail")
		}
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	waitCoordinatorDone(t, coord, 5*time.Second)

	st, _ := scheduler.State("t1")
	if st != TaskStateCompleted {
		t.Fatalf("expected completed state after retry, got %s", st)
	}
	attempt, _ := scheduler.Attempt("t1")
	if attempt != 2 {
		t.Fatalf("expected retry attempt=2, got %d", attempt)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	hasWorkerStoppedLogPath := false
	hasReplanLogPath := false
	for _, event := range events {
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		switch event.Type {
		case EventTypeWorkerStopped:
			if strings.HasPrefix(event.WorkerID, "worker-t1-") && strings.TrimSpace(toString(payload["log_path"])) != "" {
				hasWorkerStoppedLogPath = true
			}
		case EventTypeRunReplanRequested:
			if event.TaskID == "t1" && strings.TrimSpace(toString(payload["log_path"])) != "" {
				hasReplanLogPath = true
			}
		}
	}
	if !hasWorkerStoppedLogPath {
		t.Fatalf("expected worker_stopped event to include log_path")
	}
	if !hasReplanLogPath {
		t.Fatalf("expected run_replan_requested event to include log_path")
	}
}

func TestCoordinator_StopAll(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-3"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       time.Now().UTC(),
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "sleep")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if err := coord.StopAll(10 * time.Millisecond); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return workers.RunningCount() == 0
	})
	st, _ := scheduler.State("t1")
	if st != TaskStateCanceled {
		t.Fatalf("expected canceled state, got %s", st)
	}
}

func TestCoordinator_ApprovalGrantFlow(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-approval-grant"
	now := time.Now().UTC()
	manager := NewManager(base)
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			func() TaskSpec {
				ts := task("t1", nil, 1)
				ts.RiskLevel = string(RiskActiveProbe)
				return ts
			}(),
		},
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	workers.HeartbeatInterval = 20 * time.Millisecond
	broker, err := NewApprovalBroker(PermissionDefault, false, time.Minute)
	if err != nil {
		t.Fatalf("NewApprovalBroker: %v", err)
	}
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, broker)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick: %v", err)
	}
	st, _ := scheduler.State("t1")
	if st != TaskStateAwaitingApproval {
		t.Fatalf("expected awaiting approval, got %s", st)
	}
	req, ok := broker.PendingForTask("t1")
	if !ok {
		t.Fatalf("expected pending approval request")
	}
	if err := broker.Approve(req.ID, ApprovalScopeTask, "tester", "approved", now, time.Minute); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitCoordinatorDone(t, coord, 5*time.Second)
	st, _ = scheduler.State("t1")
	if st != TaskStateCompleted {
		t.Fatalf("expected completed after approval, got %s", st)
	}
}

func TestCoordinator_ApprovalDeniedBlocksTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-approval-deny"
	now := time.Now().UTC()
	manager := NewManager(base)
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			func() TaskSpec {
				ts := task("t1", nil, 1)
				ts.RiskLevel = string(RiskActiveProbe)
				return ts
			}(),
		},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	broker, _ := NewApprovalBroker(PermissionDefault, false, time.Minute)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, broker)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick: %v", err)
	}
	req, ok := broker.PendingForTask("t1")
	if !ok {
		t.Fatalf("expected pending approval")
	}
	if err := broker.Deny(req.ID, "tester", "no"); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick after deny: %v", err)
	}
	st, _ := scheduler.State("t1")
	if st != TaskStateBlocked {
		t.Fatalf("expected blocked after deny, got %s", st)
	}
}

func TestCoordinator_ApprovalExpirationBlocksTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-approval-expire"
	now := time.Now().UTC()
	manager := NewManager(base)
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			func() TaskSpec {
				ts := task("t1", nil, 1)
				ts.RiskLevel = string(RiskActiveProbe)
				return ts
			}(),
		},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	broker, _ := NewApprovalBroker(PermissionDefault, false, time.Second)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, broker)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick: %v", err)
	}
	now = now.Add(2 * time.Second)
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick after expire: %v", err)
	}
	st, _ := scheduler.State("t1")
	if st != TaskStateBlocked {
		t.Fatalf("expected blocked after expiry, got %s", st)
	}
}

func TestCoordinator_DeniesOutOfScopeBeforeApproval(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-scope-deny"
	now := time.Now().UTC()
	manager := NewManager(base)
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			func() TaskSpec {
				ts := task("t1", nil, 1)
				ts.RiskLevel = string(RiskActiveProbe)
				ts.Targets = []string{"8.8.8.8"}
				return ts
			}(),
		},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	broker, _ := NewApprovalBroker(PermissionDefault, false, time.Minute)
	coord, err := NewCoordinator(runID, Scope{Networks: []string{"192.168.50.0/24"}}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, broker)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick: %v", err)
	}
	st, _ := scheduler.State("t1")
	if st != TaskStateBlocked {
		t.Fatalf("expected blocked state for out-of-scope task, got %s", st)
	}
	if pending := broker.Pending(); len(pending) != 0 {
		t.Fatalf("expected no approval request for out-of-scope task")
	}
}

func TestCoordinator_ReconcileLeases(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-reconcile"
	now := time.Now().UTC()
	manager := NewManager(base)
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  2,
		Tasks: []TaskSpec{
			task("queued", nil, 1),
			task("running", nil, 1),
			task("awaiting", nil, 1),
		},
	}
	scheduler, _ := NewScheduler(plan, 2)
	workers := NewWorkerManager(manager)

	_ = manager.WriteLease(runID, TaskLease{
		TaskID:    "queued",
		LeaseID:   "l1",
		Status:    LeaseStatusQueued,
		Attempt:   1,
		StartedAt: now,
		Deadline:  now.Add(time.Minute),
	})
	_ = manager.WriteLease(runID, TaskLease{
		TaskID:    "running",
		LeaseID:   "l2",
		WorkerID:  "worker-running",
		Status:    LeaseStatusRunning,
		Attempt:   2,
		StartedAt: now,
		Deadline:  now.Add(time.Minute),
	})
	_ = manager.WriteLease(runID, TaskLease{
		TaskID:    "awaiting",
		LeaseID:   "l3",
		Status:    LeaseStatusAwaitingApproval,
		Attempt:   1,
		StartedAt: now,
		Deadline:  now.Add(time.Minute),
	})

	// Keep worker-running alive so reconcile maps it as running.
	workers.HeartbeatInterval = 20 * time.Millisecond
	if err := workers.Launch(runID, helperWorkerSpec(t, "worker-running", "sleep")); err != nil {
		t.Fatalf("launch running worker: %v", err)
	}
	defer func() { _ = workers.Stop(runID, "worker-running", 10*time.Millisecond) }()

	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if st, _ := scheduler.State("queued"); st != TaskStateQueued {
		t.Fatalf("queued state mismatch: %s", st)
	}
	if st, _ := scheduler.State("running"); st != TaskStateRunning {
		t.Fatalf("running state mismatch: %s", st)
	}
	if st, _ := scheduler.State("awaiting"); st != TaskStateAwaitingApproval {
		t.Fatalf("awaiting state mismatch: %s", st)
	}
	if attempt, _ := scheduler.Attempt("running"); attempt != 2 {
		t.Fatalf("running attempt mismatch: %d", attempt)
	}
}

func TestCoordinator_WorkerStopRequestCancelsTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-worker-stop"
	now := time.Now().UTC()
	manager := NewManager(base)
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "sleep")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick launch: %v", err)
	}
	if err := manager.SubmitWorkerStopRequest(runID, "worker-t1-a1", "tester", "manual stop"); err != nil {
		t.Fatalf("SubmitWorkerStopRequest: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick stop: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return workers.RunningCount() == 0
	})
	st, _ := scheduler.State("t1")
	if st != TaskStateCanceled {
		t.Fatalf("expected canceled state, got %s", st)
	}
}

func TestCoordinator_ExecutionTimeoutFailsRunningTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-timeout"
	now := time.Now().UTC()
	manager := NewManager(base)
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	ts := task("t1", nil, 1)
	ts.Budget.MaxRuntime = 100 * time.Millisecond
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{ts},
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "sleep")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick launch: %v", err)
	}
	now = now.Add(2 * time.Second)
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick timeout: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return workers.RunningCount() == 0
	})
	st, _ := scheduler.State("t1")
	if st != TaskStateFailed {
		t.Fatalf("expected failed state after execution timeout, got %s", st)
	}
	lease, err := manager.ReadLease(runID, "t1")
	if err != nil {
		t.Fatalf("ReadLease: %v", err)
	}
	if lease.Status != LeaseStatusFailed {
		t.Fatalf("expected failed lease status, got %s", lease.Status)
	}
}

func TestCoordinator_ExecutionTimeoutPausedWhileAwaitingApproval(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-timeout-awaiting"
	now := time.Now().UTC()
	manager := NewManager(base)
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	ts := task("t1", nil, 1)
	ts.RiskLevel = string(RiskActiveProbe)
	ts.Budget.MaxRuntime = 100 * time.Millisecond
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{ts},
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	broker, err := NewApprovalBroker(PermissionDefault, false, time.Minute)
	if err != nil {
		t.Fatalf("NewApprovalBroker: %v", err)
	}
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, broker)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick awaiting approval: %v", err)
	}
	st, _ := scheduler.State("t1")
	if st != TaskStateAwaitingApproval {
		t.Fatalf("expected awaiting approval state, got %s", st)
	}
	now = now.Add(10 * time.Second)
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick while awaiting approval: %v", err)
	}
	st, _ = scheduler.State("t1")
	if st != TaskStateAwaitingApproval {
		t.Fatalf("expected awaiting approval to remain (timeout paused), got %s", st)
	}
}

func TestCoordinator_EmitsReplanForBlockedTaskWithoutDuplicates(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-replan-blocked"
	manager := NewManager(base)
	now := time.Now().UTC()
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			func() TaskSpec {
				ts := task("t1", nil, 1)
				ts.Targets = []string{"8.8.8.8"}
				ts.RiskLevel = string(RiskActiveProbe)
				return ts
			}(),
		},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	broker, _ := NewApprovalBroker(PermissionDefault, false, time.Minute)
	coord, err := NewCoordinator(runID, Scope{Networks: []string{"192.168.50.0/24"}}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, broker)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick2: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick3: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if count := replanEventCount(events, "blocked_task"); count != 1 {
		t.Fatalf("expected 1 blocked_task replan event, got %d", count)
	}
	if count := eventCount(events, EventTypeRunStateUpdated); count != 2 {
		t.Fatalf("expected 2 run_state_updated events (initial + state transition), got %d", count)
	}
	statePath := filepath.Join(BuildRunPaths(base, runID).PlanDir, "state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state file at %s: %v", statePath, err)
	}
}

func TestCoordinator_EmitsReplanForNewFindingWithoutDuplicates(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-replan-finding"
	manager := NewManager(base)
	now := time.Now().UTC()
	manager.Now = func() time.Time { return now }
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-finding-1",
		RunID:    runID,
		WorkerID: "worker-x",
		TaskID:   "t1",
		Seq:      1,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "192.168.50.77",
			"finding_type": "open_port",
			"title":        "SSH open",
			"location":     "22/tcp",
			"severity":     "low",
			"confidence":   "high",
		}),
	}); err != nil {
		t.Fatalf("append finding event: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			func() TaskSpec {
				ts := task("t1", nil, 1)
				ts.RiskLevel = string(RiskActiveProbe)
				return ts
			}(),
		},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	broker, _ := NewApprovalBroker(PermissionDefault, false, time.Minute)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, broker)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick2: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if count := replanEventCount(events, "new_finding"); count != 1 {
		t.Fatalf("expected 1 new_finding replan event, got %d", count)
	}
}

func TestCoordinator_NoSilentRetryEmitsReplanEvent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-retry-visible"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       time.Now().UTC(),
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	scheduler, _ := NewScheduler(plan, 1)
	scheduler.SetRetryBackoff([]time.Duration{20 * time.Millisecond})
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		if attempt == 1 {
			return helperWorkerSpec(t, workerID, "fail")
		}
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	waitCoordinatorDone(t, coord, 5*time.Second)
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if count := replanEventCount(events, "retry_scheduled"); count < 1 {
		t.Fatalf("expected retry_scheduled replan event")
	}
}

func TestCoordinator_ReplanTriggerApprovalDeniedAndExpired(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-replan-approval-events"
	manager := NewManager(base)
	now := time.Now().UTC()
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-appr-deny",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   "t1",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeApprovalDenied,
		Payload:  mustJSONRaw(map[string]any{"reason": "operator denied"}),
	}); err != nil {
		t.Fatalf("append approval denied: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-appr-expire",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   "t2",
		Seq:      3,
		TS:       now.Add(2 * time.Second),
		Type:     EventTypeApprovalExpired,
		Payload:  mustJSONRaw(map[string]any{"reason": "timeout"}),
	}); err != nil {
		t.Fatalf("append approval expired: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick2: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if count := replanEventCount(events, "approval_denied"); count != 1 {
		t.Fatalf("expected one approval_denied replan trigger, got %d", count)
	}
	if count := replanEventCount(events, "approval_expired"); count != 1 {
		t.Fatalf("expected one approval_expired replan trigger, got %d", count)
	}
	if count := replanOutcomeCount(events, "terminate_run"); count < 2 {
		t.Fatalf("expected terminate_run outcomes for approval replan events")
	}
}

func TestCoordinator_ReplanTriggerMissingArtifactsAfterRetries(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-replan-missing-artifacts"
	manager := NewManager(base)
	now := time.Now().UTC()
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-fail-1",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   "t1",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFailed,
		Payload:  mustJSONRaw(map[string]any{"reason": "missing_required_artifacts"}),
	}); err != nil {
		t.Fatalf("append fail1: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-fail-2",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   "t1",
		Seq:      3,
		TS:       now.Add(2 * time.Second),
		Type:     EventTypeTaskFailed,
		Payload:  mustJSONRaw(map[string]any{"reason": "missing_required_artifacts"}),
	}); err != nil {
		t.Fatalf("append fail2: %v", err)
	}

	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick2: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if count := replanEventCount(events, "missing_required_artifacts_after_retries"); count != 1 {
		t.Fatalf("expected one missing artifacts replan trigger, got %d", count)
	}
	if count := replanOutcomeCount(events, "split_task"); count < 1 {
		t.Fatalf("expected split_task outcome for missing artifacts after retries")
	}
}

func TestCoordinator_ReplanTriggerRepeatedStepLoop(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-replan-loop"
	manager := NewManager(base)
	now := time.Now().UTC()
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-loop-fail",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   "t1",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFailed,
		Payload:  mustJSONRaw(map[string]any{"reason": "repeated_step_loop"}),
	}); err != nil {
		t.Fatalf("append loop fail: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{task("t1", nil, 1)},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick: %v", err)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if count := replanEventCount(events, "repeated_step_loop"); count != 1 {
		t.Fatalf("expected repeated_step_loop trigger, got %d", count)
	}
}

func TestCoordinator_ReplanTriggerWorkerRecovery(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-replan-worker-recovery"
	manager := NewManager(base)
	now := time.Now().UTC()
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-recovery-fail",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   "t1",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFailed,
		Payload:  mustJSONRaw(map[string]any{"reason": "stale_lease"}),
	}); err != nil {
		t.Fatalf("append stale lease fail: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{task("t1", nil, 1)},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick: %v", err)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if count := replanEventCount(events, "worker_recovery"); count != 1 {
		t.Fatalf("expected worker_recovery trigger, got %d", count)
	}
}

func TestCoordinator_ReplanTriggerExecutionFailure(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-replan-execution-failure"
	manager := NewManager(base)
	now := time.Now().UTC()
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-exec-fail",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		TaskID:   "t1",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFailed,
		Payload:  mustJSONRaw(map[string]any{"reason": WorkerFailureCommandTimeout}),
	}); err != nil {
		t.Fatalf("append command timeout fail: %v", err)
	}
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{task("t1", nil, 1)},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick: %v", err)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if count := replanEventCount(events, "execution_failure"); count != 1 {
		t.Fatalf("expected execution_failure trigger, got %d", count)
	}
}

func TestCoordinator_AdaptiveReplanExecutionFailureDedupesRepeatedCommandFailed(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-adaptive-exec-failure-dedupe"
	manager := NewManager(base)
	adaptiveA := task("task-rp-a", nil, 90)
	adaptiveA.Strategy = "adaptive_replan_execution_failure"
	adaptiveA.Action = assistAction("recover from command failure")
	adaptiveB := task("task-rp-b", nil, 90)
	adaptiveB.Strategy = "adaptive_replan_execution_failure"
	adaptiveB.Action = assistAction("recover from command failure")
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
			adaptiveA,
			adaptiveB,
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, plan.Scope, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	failPayload := map[string]any{
		"reason":  WorkerFailureCommandFailed,
		"command": "curl",
		"args":    []string{"-fsS", "http://127.0.0.1:65535"},
		"error":   "exit status 7",
	}
	if err := manager.EmitEvent(runID, "signal-worker-a", adaptiveA.TaskID, EventTypeTaskFailed, failPayload); err != nil {
		t.Fatalf("EmitEvent adaptiveA failure: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-b", adaptiveB.TaskID, EventTypeTaskFailed, failPayload); err != nil {
		t.Fatalf("EmitEvent adaptiveB failure: %v", err)
	}
	if err := coord.handleEventDrivenReplanTriggers(); err != nil {
		t.Fatalf("handleEventDrivenReplanTriggers: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	replanEvents := replanEventsByTrigger(events, "execution_failure")
	if len(replanEvents) != 2 {
		t.Fatalf("expected 2 execution_failure replan events, got %d", len(replanEvents))
	}
	taskAdded := 0
	duplicateIgnored := 0
	addedTaskIDs := map[string]struct{}{}
	for _, replan := range replanEvents {
		payload := map[string]any{}
		if len(replan.Payload) > 0 {
			_ = json.Unmarshal(replan.Payload, &payload)
		}
		switch toString(payload["mutation_status"]) {
		case "task_added":
			taskAdded++
			if id := toString(payload["added_task_id"]); id != "" {
				addedTaskIDs[id] = struct{}{}
			}
		case "duplicate_ignored":
			duplicateIgnored++
		}
	}
	if taskAdded != 1 {
		t.Fatalf("expected exactly one task_added mutation, got %d", taskAdded)
	}
	if duplicateIgnored != 1 {
		t.Fatalf("expected exactly one duplicate_ignored mutation, got %d", duplicateIgnored)
	}
	if len(addedTaskIDs) != 1 {
		t.Fatalf("expected exactly one added adaptive task id, got %d", len(addedTaskIDs))
	}
	if coord.replanMutationCount != 1 {
		t.Fatalf("expected one replan mutation count, got %d", coord.replanMutationCount)
	}
}

func TestCoordinator_AdaptiveReplanAddsTaskAndLease(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-adaptive-add"
	manager := NewManager(base)
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{task("t1", nil, 1)},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, plan.Scope, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", EventTypeTaskFailed, map[string]any{
		"reason": "repeated_step_loop",
	}); err != nil {
		t.Fatalf("EmitEvent task_failed: %v", err)
	}
	if err := coord.handleEventDrivenReplanTriggers(); err != nil {
		t.Fatalf("handleEventDrivenReplanTriggers: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	replan := latestReplanEventByTrigger(events, "repeated_step_loop")
	if replan == nil {
		t.Fatalf("expected replan event for repeated_step_loop")
	}
	payload := map[string]any{}
	if len(replan.Payload) > 0 {
		_ = json.Unmarshal(replan.Payload, &payload)
	}
	if mutated, _ := payload["graph_mutation"].(bool); !mutated {
		t.Fatalf("expected graph_mutation=true payload: %#v", payload)
	}
	addedTaskID := toString(payload["added_task_id"])
	if addedTaskID == "" {
		t.Fatalf("expected added_task_id in payload: %#v", payload)
	}
	if _, ok := scheduler.Task(addedTaskID); !ok {
		t.Fatalf("expected added task %s in scheduler", addedTaskID)
	}
	addedTask, err := manager.ReadTask(runID, addedTaskID)
	if err != nil {
		t.Fatalf("ReadTask adaptive task: %v", err)
	}
	if addedTask.IdempotencyKey == "" {
		t.Fatalf("expected idempotency key on adaptive task")
	}
	lease, err := manager.ReadLease(runID, addedTaskID)
	if err != nil {
		t.Fatalf("ReadLease adaptive task: %v", err)
	}
	if lease.Status != LeaseStatusQueued {
		t.Fatalf("expected queued lease for adaptive task, got %s", lease.Status)
	}
}

func TestCoordinator_AdaptiveReplanHonorsBudgetCap(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-adaptive-budget"
	manager := NewManager(base)
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"max_replans=1"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{task("t1", nil, 1)},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, plan.Scope, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	coord.ensureReplanBudget()
	if coord.replanMutationBudget != 1 {
		t.Fatalf("expected replan budget=1, got %d", coord.replanMutationBudget)
	}

	if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", EventTypeTaskFailed, map[string]any{
		"reason": "repeated_step_loop",
	}); err != nil {
		t.Fatalf("emit first failure: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", EventTypeTaskFailed, map[string]any{
		"reason": "repeated_step_loop",
	}); err != nil {
		t.Fatalf("emit second failure: %v", err)
	}
	if err := coord.handleEventDrivenReplanTriggers(); err != nil {
		t.Fatalf("handleEventDrivenReplanTriggers: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	replanEvents := replanEventsByTrigger(events, "repeated_step_loop")
	if len(replanEvents) < 2 {
		t.Fatalf("expected at least two repeated_step_loop replan events, got %d", len(replanEvents))
	}
	firstPayload := map[string]any{}
	_ = json.Unmarshal(replanEvents[0].Payload, &firstPayload)
	if mutated, _ := firstPayload["graph_mutation"].(bool); !mutated {
		t.Fatalf("expected first event to mutate graph: %#v", firstPayload)
	}
	secondPayload := map[string]any{}
	_ = json.Unmarshal(replanEvents[1].Payload, &secondPayload)
	if status := toString(secondPayload["mutation_status"]); status != "replan_budget_exhausted" {
		t.Fatalf("expected replan_budget_exhausted status, got %q payload=%#v", status, secondPayload)
	}
	if outcome := toString(secondPayload["outcome"]); outcome != string(ReplanOutcomeTerminate) {
		t.Fatalf("expected terminate outcome, got %q", outcome)
	}
	status, err := manager.Status(runID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "stopped" {
		t.Fatalf("expected run stopped after replan budget exhaustion, got %s", status.State)
	}
}

func TestCoordinator_OperatorInstructionTriggersAdaptiveTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-operator-instruction"
	manager := NewManager(base)
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	if err := manager.EmitEvent(runID, "operator", "", EventTypeOperatorInstruction, map[string]any{
		"instruction": "map services and summarize weak points",
	}); err != nil {
		t.Fatalf("EmitEvent operator_instruction: %v", err)
	}
	if err := coord.handleEventDrivenReplanTriggers(); err != nil {
		t.Fatalf("handleEventDrivenReplanTriggers: %v", err)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	replan := latestReplanEventByTrigger(events, "operator_instruction")
	if replan == nil {
		t.Fatalf("expected replan event for operator_instruction")
	}
	payload := map[string]any{}
	if len(replan.Payload) > 0 {
		_ = json.Unmarshal(replan.Payload, &payload)
	}
	if toString(payload["mutation_status"]) != "task_added" {
		t.Fatalf("expected task_added mutation status, got %q", toString(payload["mutation_status"]))
	}
	addedTaskID := toString(payload["added_task_id"])
	if addedTaskID == "" {
		t.Fatalf("expected added_task_id in payload")
	}
	addedTask, err := manager.ReadTask(runID, addedTaskID)
	if err != nil {
		t.Fatalf("ReadTask added task: %v", err)
	}
	if addedTask.Action.Type != "assist" {
		t.Fatalf("expected assist action for operator instruction task, got %q", addedTask.Action.Type)
	}
	if !strings.Contains(strings.ToLower(addedTask.Goal), "map services") {
		t.Fatalf("unexpected added task goal: %q", addedTask.Goal)
	}
}

func TestRetryableWorkerFailureReason(t *testing.T) {
	t.Parallel()

	if retryableWorkerFailureReason(WorkerFailureCommandInterrupted) {
		t.Fatalf("expected command_interrupted to be non-retryable")
	}
	if !retryableWorkerFailureReason(WorkerFailureAssistLoopDetected) {
		t.Fatalf("expected assist_loop_detected to be retryable for first occurrence")
	}
	if !retryableWorkerFailureReason(WorkerFailureCommandTimeout) {
		t.Fatalf("expected command_timeout to be retryable")
	}
}

func TestHasRepeatedTaskFailureReason(t *testing.T) {
	t.Parallel()

	runID := "run-repeat-reason"
	taskID := "task-1"
	now := time.Now().UTC()

	makeFailed := func(id string, offset time.Duration, reason string) EventEnvelope {
		return EventEnvelope{
			EventID:  id,
			RunID:    runID,
			WorkerID: "signal-worker-task-1-a1",
			TaskID:   taskID,
			Seq:      1,
			TS:       now.Add(offset),
			Type:     EventTypeTaskFailed,
			Payload:  mustJSONRaw(map[string]any{"reason": reason}),
		}
	}

	events := []EventEnvelope{
		makeFailed("e1", time.Second, WorkerFailureAssistNoAction),
		makeFailed("e2", 2*time.Second, WorkerFailureAssistLoopDetected),
	}
	if hasRepeatedTaskFailureReason(events, taskID, WorkerFailureAssistLoopDetected) {
		t.Fatalf("expected first assist_loop_detected occurrence to not be repeated")
	}

	events = append(events, makeFailed("e3", 3*time.Second, WorkerFailureAssistLoopDetected))
	if !hasRepeatedTaskFailureReason(events, taskID, WorkerFailureAssistLoopDetected) {
		t.Fatalf("expected repeated assist_loop_detected reason to be detected")
	}
}

func TestAllowAssistLoopRetry(t *testing.T) {
	t.Parallel()

	if !allowAssistLoopRetry(TaskSpec{Strategy: "recon_seed"}) {
		t.Fatalf("expected recon_seed strategy to allow assist loop retry")
	}
	if allowAssistLoopRetry(TaskSpec{Strategy: "hypothesis_validate"}) {
		t.Fatalf("did not expect hypothesis_validate strategy to allow assist loop retry")
	}
	if allowAssistLoopRetry(TaskSpec{Strategy: "adaptive_replan_execution_failure"}) {
		t.Fatalf("did not expect adaptive replan strategy to allow assist loop retry")
	}
}

func TestCoordinator_BudgetGuardExhaustsStepBudget(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-budget-steps"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       time.Now().UTC(),
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	ts := task("t1", nil, 1)
	ts.Budget.MaxSteps = 1
	plan := RunPlan{
		RunID:           runID,
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{ts},
	}
	scheduler, _ := NewScheduler(plan, 1)
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 2, 2*time.Second, 4*time.Second, 3*time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "sleep")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	if err := coord.Tick(); err != nil {
		t.Fatalf("tick launch: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", EventTypeTaskProgress, map[string]any{
		"steps": 2,
	}); err != nil {
		t.Fatalf("emit progress: %v", err)
	}
	if err := coord.Tick(); err != nil {
		t.Fatalf("tick budget: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return workers.RunningCount() == 0
	})
	st, _ := scheduler.State("t1")
	if st != TaskStateFailed {
		t.Fatalf("expected failed state after budget exhaustion, got %s", st)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if count := replanEventCount(events, "budget_exhausted"); count < 1 {
		t.Fatalf("expected budget_exhausted replan event")
	}
}

func TestCoordinator_TerminalizesAfterStaleRunningTaskWithDeadDependency(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-coord-stale-terminalization"
	manager := NewManager(base)
	now := time.Now().UTC()
	manager.Now = func() time.Time { return now }

	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 2),
			task("t2", []string{"t1"}, 1),
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scheduler, err := NewScheduler(plan, 1)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := scheduler.MarkLeased("t1"); err != nil {
		t.Fatalf("MarkLeased t1: %v", err)
	}
	if err := scheduler.MarkRunning("t1"); err != nil {
		t.Fatalf("MarkRunning t1: %v", err)
	}
	workers := NewWorkerManager(manager)
	coord, err := NewCoordinator(runID, Scope{}, manager, workers, scheduler, 1, time.Second, time.Second, time.Second, func(task TaskSpec, attempt int, workerID string) WorkerSpec {
		return helperWorkerSpec(t, workerID, "ok")
	}, nil)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	lease := TaskLease{
		TaskID:    "t1",
		LeaseID:   "lease-t1-a1",
		WorkerID:  "worker-t1-a1",
		Status:    LeaseStatusRunning,
		Attempt:   1,
		StartedAt: now.Add(-2 * time.Second),
		Deadline:  now.Add(2 * time.Second),
	}
	if err := manager.WriteLease(runID, lease); err != nil {
		t.Fatalf("WriteLease t1: %v", err)
	}

	if err := coord.handleStaleReclaims(); err != nil {
		t.Fatalf("handleStaleReclaims: %v", err)
	}
	stateT1, _ := scheduler.State("t1")
	if stateT1 != TaskStateFailed {
		t.Fatalf("expected t1 failed after stale reclaim, got %s", stateT1)
	}
	if !coord.Done() {
		t.Fatalf("expected coordinator done when only remaining queued task depends on failed task")
	}
}

func waitCoordinatorDone(t *testing.T, coord *Coordinator, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := coord.Tick(); err != nil {
			t.Fatalf("coordinator tick: %v", err)
		}
		if coord.Done() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("coordinator did not complete within %s", timeout)
}

func replanEventCount(events []EventEnvelope, trigger string) int {
	count := 0
	for _, event := range events {
		if event.Type != EventTypeRunReplanRequested {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if toString(payload["trigger"]) == trigger {
			count++
		}
	}
	return count
}

func eventCount(events []EventEnvelope, eventType string) int {
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func replanOutcomeCount(events []EventEnvelope, outcome string) int {
	count := 0
	for _, event := range events {
		if event.Type != EventTypeRunReplanRequested {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if toString(payload["outcome"]) == outcome {
			count++
		}
	}
	return count
}

func latestReplanEventByTrigger(events []EventEnvelope, trigger string) *EventEnvelope {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != EventTypeRunReplanRequested {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if toString(payload["trigger"]) == trigger {
			return &event
		}
	}
	return nil
}

func replanEventsByTrigger(events []EventEnvelope, trigger string) []EventEnvelope {
	out := []EventEnvelope{}
	for _, event := range events {
		if event.Type != EventTypeRunReplanRequested {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if toString(payload["trigger"]) == trigger {
			out = append(out, event)
		}
	}
	return out
}

func latestTaskFailedByReason(events []EventEnvelope, reason string) *EventEnvelope {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != EventTypeTaskFailed {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if toString(payload["reason"]) == reason {
			return &event
		}
	}
	return nil
}
