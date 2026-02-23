package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerStartStatusStopLifecycle(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan.json")
	plan := RunPlan{
		RunID:           "run-1",
		Scope:           Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"report_generated"},
		StopCriteria:    []string{"out_of_scope"},
		MaxParallelism:  2,
		Tasks: []TaskSpec{
			{
				TaskID:            "task-1",
				Goal:              "scan target",
				DoneWhen:          []string{"scan_log_exists"},
				FailWhen:          []string{"target_unreachable"},
				ExpectedArtifacts: []string{"logs/nmap.txt"},
				RiskLevel:         "active_probe",
				Budget: TaskBudget{
					MaxSteps:     8,
					MaxToolCalls: 8,
					MaxRuntime:   5 * time.Minute,
				},
			},
		},
	}
	writePlan(t, planPath, plan)

	m := NewManager(base)
	startedRunID, err := m.Start(planPath, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if startedRunID != "run-1" {
		t.Fatalf("run id mismatch: got %s", startedRunID)
	}

	planStored := filepath.Join(base, "run-1", "orchestrator", "plan", "plan.json")
	if _, err := os.Stat(planStored); err != nil {
		t.Fatalf("expected stored plan: %v", err)
	}
	taskStored := filepath.Join(base, "run-1", "orchestrator", "task", "task-1.json")
	if _, err := os.Stat(taskStored); err != nil {
		t.Fatalf("expected stored task: %v", err)
	}

	status, err := m.Status("run-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "running" {
		t.Fatalf("state mismatch: got %s", status.State)
	}

	if err := m.Stop("run-1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	status, err = m.Status("run-1")
	if err != nil {
		t.Fatalf("Status after stop: %v", err)
	}
	if status.State != "stopped" {
		t.Fatalf("state mismatch after stop: got %s", status.State)
	}
}

func TestBuildRunStatusIgnoresApprovalEventsWithoutTaskID(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []EventEnvelope{
		{EventID: "e1", RunID: "run-approval-taskless", WorkerID: orchestratorWorkerID, Seq: 1, TS: now, Type: EventTypeRunStarted},
		{EventID: "e2", RunID: "run-approval-taskless", WorkerID: "worker-1", TaskID: "task-1", Seq: 1, TS: now.Add(time.Second), Type: EventTypeTaskStarted},
		{EventID: "e3", RunID: "run-approval-taskless", WorkerID: "worker-1", TaskID: "task-1", Seq: 2, TS: now.Add(2 * time.Second), Type: EventTypeTaskCompleted},
		{EventID: "e4", RunID: "run-approval-taskless", WorkerID: "operator-approval-1", Seq: 1, TS: now.Add(3 * time.Second), Type: EventTypeApprovalGranted},
		{EventID: "e5", RunID: "run-approval-taskless", WorkerID: orchestratorWorkerID, Seq: 2, TS: now.Add(4 * time.Second), Type: EventTypeRunStopped},
	}

	status := BuildRunStatus("run-approval-taskless", events)
	if status.State != "stopped" {
		t.Fatalf("expected stopped state, got %s", status.State)
	}
	if status.RunningTasks != 0 {
		t.Fatalf("expected no running tasks, got %d", status.RunningTasks)
	}
	if status.QueuedTasks != 0 {
		t.Fatalf("expected no queued tasks, got %d", status.QueuedTasks)
	}
}

func TestManagerStatusIgnoresApprovalEventsWithoutTaskID(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-status-approval-taskless"
	paths, err := EnsureRunLayout(base, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	eventPath := filepath.Join(paths.EventDir, "event.jsonl")
	now := time.Now().UTC()

	writeEvent(t, eventPath, EventEnvelope{EventID: "e1", RunID: runID, WorkerID: orchestratorWorkerID, Seq: 1, TS: now, Type: EventTypeRunStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e2", RunID: runID, WorkerID: "worker-1", TaskID: "task-1", Seq: 1, TS: now.Add(time.Second), Type: EventTypeTaskStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e3", RunID: runID, WorkerID: "worker-1", TaskID: "task-1", Seq: 2, TS: now.Add(2 * time.Second), Type: EventTypeTaskCompleted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e4", RunID: runID, WorkerID: "operator-approval-1", Seq: 1, TS: now.Add(3 * time.Second), Type: EventTypeApprovalGranted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e5", RunID: runID, WorkerID: orchestratorWorkerID, Seq: 2, TS: now.Add(4 * time.Second), Type: EventTypeRunStopped})

	m := NewManager(base)
	status, err := m.Status(runID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "stopped" {
		t.Fatalf("expected stopped state, got %s", status.State)
	}
	if status.RunningTasks != 0 {
		t.Fatalf("expected no running tasks, got %d", status.RunningTasks)
	}
	if status.QueuedTasks != 0 {
		t.Fatalf("expected no queued tasks, got %d", status.QueuedTasks)
	}
}

func TestManagerStatusApprovalEventsDoNotMarkTaskRunning(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-status-approval-state"
	paths, err := EnsureRunLayout(base, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	eventPath := filepath.Join(paths.EventDir, "event.jsonl")
	now := time.Now().UTC()

	writeEvent(t, eventPath, EventEnvelope{EventID: "e1", RunID: runID, WorkerID: orchestratorWorkerID, Seq: 1, TS: now, Type: EventTypeRunStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e2", RunID: runID, WorkerID: orchestratorWorkerID, TaskID: "task-approval", Seq: 2, TS: now.Add(time.Second), Type: EventTypeTaskLeased})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e3", RunID: runID, WorkerID: orchestratorWorkerID, TaskID: "task-approval", Seq: 3, TS: now.Add(2 * time.Second), Type: EventTypeApprovalRequested})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e4", RunID: runID, WorkerID: orchestratorWorkerID, TaskID: "task-approval", Seq: 4, TS: now.Add(3 * time.Second), Type: EventTypeApprovalExpired})

	m := NewManager(base)
	status, err := m.Status(runID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.RunningTasks != 0 {
		t.Fatalf("approval-only task should not be counted as running, got %d", status.RunningTasks)
	}
	if status.QueuedTasks != 1 {
		t.Fatalf("expected queued task count 1, got %d", status.QueuedTasks)
	}
}

func TestManagerWorkersAndEvents(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-2"
	paths, err := EnsureRunLayout(base, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	eventPath := filepath.Join(paths.EventDir, "event.jsonl")
	now := time.Now().UTC()

	writeEvent(t, eventPath, EventEnvelope{EventID: "e1", RunID: runID, WorkerID: orchestratorWorkerID, Seq: 1, TS: now, Type: EventTypeRunStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e2", RunID: runID, WorkerID: "worker-1", Seq: 1, TS: now.Add(time.Second), Type: EventTypeWorkerStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e3", RunID: runID, WorkerID: "worker-1", TaskID: "t1", Seq: 2, TS: now.Add(2 * time.Second), Type: EventTypeTaskStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e4", RunID: runID, WorkerID: "worker-1", TaskID: "t1", Seq: 3, TS: now.Add(3 * time.Second), Type: EventTypeTaskCompleted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e5", RunID: runID, WorkerID: "worker-1", Seq: 4, TS: now.Add(4 * time.Second), Type: EventTypeWorkerStopped})

	m := NewManager(base)
	workers, err := m.Workers(runID)
	if err != nil {
		t.Fatalf("Workers: %v", err)
	}
	if len(workers) == 0 {
		t.Fatalf("expected workers")
	}
	found := false
	for _, worker := range workers {
		if worker.WorkerID == "worker-1" {
			found = true
			if worker.State != "stopped" {
				t.Fatalf("worker state mismatch: got %s", worker.State)
			}
		}
	}
	if !found {
		t.Fatalf("worker-1 not found")
	}

	events, err := m.Events(runID, 2)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("limit mismatch: got %d", len(events))
	}
}

func TestValidatePlanForStartRequiresScopeAndConstraints(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "r1",
		SuccessCriteria: []string{"ok"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "g",
				DoneWhen:          []string{"d"},
				FailWhen:          []string{"f"},
				ExpectedArtifacts: []string{"a"},
				RiskLevel:         "recon_readonly",
				Budget:            TaskBudget{MaxSteps: 1, MaxToolCalls: 1, MaxRuntime: time.Second},
			},
		},
	}
	if err := ValidatePlanForStart(plan); err == nil {
		t.Fatalf("expected validation error")
	}
}

func writePlan(t *testing.T, path string, plan RunPlan) {
	t.Helper()
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
}

func writeEvent(t *testing.T, path string, event EventEnvelope) {
	t.Helper()
	if err := AppendEventJSONL(path, event); err != nil {
		t.Fatalf("append event: %v", err)
	}
}
