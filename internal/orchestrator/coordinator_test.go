package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
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
