package orchestrator

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWriteLease_WritesFileAndEvent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-lease-1"
	m := NewManager(base)
	now := time.Now().UTC()
	m.Now = func() time.Time { return now }

	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(m.eventPath(runID), EventEnvelope{
		EventID:  "e-run-start",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}

	lease := TaskLease{
		TaskID:    "task-1",
		LeaseID:   "lease-1",
		WorkerID:  "worker-1",
		Status:    LeaseStatusLeased,
		Attempt:   1,
		StartedAt: now,
		Deadline:  now.Add(2 * time.Minute),
	}
	if err := m.WriteLease(runID, lease); err != nil {
		t.Fatalf("WriteLease: %v", err)
	}

	leases, err := m.ReadLeases(runID)
	if err != nil {
		t.Fatalf("ReadLeases: %v", err)
	}
	if len(leases) != 1 {
		t.Fatalf("expected 1 lease, got %d", len(leases))
	}
	events, err := m.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Type == EventTypeTaskLeased && event.TaskID == "task-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("task_leased event not found")
	}
}

func TestReclaimMissedStartup_RequeuesLease(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-lease-2"
	startedAt := time.Now().UTC().Add(-2 * time.Minute)
	m := NewManager(base)
	m.Now = func() time.Time { return startedAt.Add(3 * time.Minute) }

	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := AppendEventJSONL(m.eventPath(runID), EventEnvelope{
		EventID:  "e1",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       startedAt,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	lease := TaskLease{
		TaskID:    "task-1",
		LeaseID:   "lease-1",
		WorkerID:  "worker-1",
		Status:    LeaseStatusLeased,
		Attempt:   1,
		StartedAt: startedAt,
		Deadline:  startedAt.Add(30 * time.Second),
	}
	if err := m.WriteLease(runID, lease); err != nil {
		t.Fatalf("WriteLease: %v", err)
	}

	reclaimed, err := m.ReclaimMissedStartup(runID, 30*time.Second)
	if err != nil {
		t.Fatalf("ReclaimMissedStartup: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("expected 1 reclaimed lease, got %d", len(reclaimed))
	}
	if reclaimed[0].Status != LeaseStatusQueued {
		t.Fatalf("expected reclaimed status queued, got %s", reclaimed[0].Status)
	}
	if reclaimed[0].WorkerID != "" {
		t.Fatalf("expected worker id cleared, got %q", reclaimed[0].WorkerID)
	}

	leases, err := m.ReadLeases(runID)
	if err != nil {
		t.Fatalf("ReadLeases: %v", err)
	}
	if len(leases) != 1 {
		t.Fatalf("expected one lease")
	}
	if leases[0].Status != LeaseStatusQueued {
		t.Fatalf("expected persisted status queued, got %s", leases[0].Status)
	}

	events, err := m.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var failed, leasedAgain bool
	for _, event := range events {
		if event.Type == EventTypeTaskFailed && event.TaskID == "task-1" {
			failed = true
		}
		if event.Type == EventTypeTaskLeased && event.TaskID == "task-1" && event.EventID != "e1" {
			leasedAgain = true
		}
	}
	if !failed {
		t.Fatalf("expected task_failed reclaim event")
	}
	if !leasedAgain {
		t.Fatalf("expected task_leased requeue event")
	}
}

func TestReclaimMissedStartup_SkipsWhenTaskStarted(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-lease-3"
	startedAt := time.Now().UTC().Add(-2 * time.Minute)
	m := NewManager(base)
	m.Now = func() time.Time { return startedAt.Add(3 * time.Minute) }

	paths, err := EnsureRunLayout(base, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	eventPath := filepath.Join(paths.EventDir, "event.jsonl")
	writeEvent(t, eventPath, EventEnvelope{EventID: "e1", RunID: runID, WorkerID: orchestratorWorkerID, Seq: 1, TS: startedAt, Type: EventTypeRunStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e2", RunID: runID, WorkerID: "worker-1", TaskID: "task-1", Seq: 1, TS: startedAt.Add(5 * time.Second), Type: EventTypeTaskStarted})

	lease := TaskLease{
		TaskID:    "task-1",
		LeaseID:   "lease-1",
		WorkerID:  "worker-1",
		Status:    LeaseStatusLeased,
		Attempt:   1,
		StartedAt: startedAt,
		Deadline:  startedAt.Add(30 * time.Second),
	}
	if err := m.WriteLease(runID, lease); err != nil {
		t.Fatalf("WriteLease: %v", err)
	}
	reclaimed, err := m.ReclaimMissedStartup(runID, 30*time.Second)
	if err != nil {
		t.Fatalf("ReclaimMissedStartup: %v", err)
	}
	if len(reclaimed) != 0 {
		t.Fatalf("expected no reclaimed lease, got %d", len(reclaimed))
	}
}

func TestReclaimStaleLeases_RequeuesWhenNoHeartbeatAndWorkerNotRunning(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-lease-stale"
	startedAt := time.Now().UTC().Add(-2 * time.Minute)
	m := NewManager(base)
	m.Now = func() time.Time { return startedAt.Add(3 * time.Minute) }

	paths, err := EnsureRunLayout(base, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	eventPath := filepath.Join(paths.EventDir, "event.jsonl")
	writeEvent(t, eventPath, EventEnvelope{EventID: "e1", RunID: runID, WorkerID: orchestratorWorkerID, Seq: 1, TS: startedAt, Type: EventTypeRunStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e2", RunID: runID, WorkerID: "worker-1", TaskID: "task-1", Seq: 1, TS: startedAt.Add(2 * time.Second), Type: EventTypeTaskStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e3", RunID: runID, WorkerID: "worker-1", Seq: 2, TS: startedAt.Add(3 * time.Second), Type: EventTypeWorkerHeartbeat})

	lease := TaskLease{
		TaskID:    "task-1",
		LeaseID:   "lease-1",
		WorkerID:  "worker-1",
		Status:    LeaseStatusRunning,
		Attempt:   1,
		StartedAt: startedAt,
		Deadline:  startedAt.Add(30 * time.Second),
	}
	if err := m.WriteLease(runID, lease); err != nil {
		t.Fatalf("WriteLease: %v", err)
	}

	reclaimed, err := m.ReclaimStaleLeases(runID, 30*time.Second, func(string) bool { return false })
	if err != nil {
		t.Fatalf("ReclaimStaleLeases: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("expected one reclaimed lease, got %d", len(reclaimed))
	}
	if reclaimed[0].Status != LeaseStatusQueued {
		t.Fatalf("expected queued status, got %s", reclaimed[0].Status)
	}
}

func TestReclaimStaleLeases_SkipsWhenWorkerRunning(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-lease-stale-running"
	startedAt := time.Now().UTC().Add(-2 * time.Minute)
	m := NewManager(base)
	m.Now = func() time.Time { return startedAt.Add(3 * time.Minute) }

	paths, err := EnsureRunLayout(base, runID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	eventPath := filepath.Join(paths.EventDir, "event.jsonl")
	writeEvent(t, eventPath, EventEnvelope{EventID: "e1", RunID: runID, WorkerID: orchestratorWorkerID, Seq: 1, TS: startedAt, Type: EventTypeRunStarted})
	writeEvent(t, eventPath, EventEnvelope{EventID: "e2", RunID: runID, WorkerID: "worker-1", TaskID: "task-1", Seq: 1, TS: startedAt.Add(2 * time.Second), Type: EventTypeTaskStarted})

	lease := TaskLease{
		TaskID:    "task-1",
		LeaseID:   "lease-1",
		WorkerID:  "worker-1",
		Status:    LeaseStatusRunning,
		Attempt:   1,
		StartedAt: startedAt,
		Deadline:  startedAt.Add(30 * time.Second),
	}
	if err := m.WriteLease(runID, lease); err != nil {
		t.Fatalf("WriteLease: %v", err)
	}

	reclaimed, err := m.ReclaimStaleLeases(runID, 30*time.Second, func(string) bool { return true })
	if err != nil {
		t.Fatalf("ReclaimStaleLeases: %v", err)
	}
	if len(reclaimed) != 0 {
		t.Fatalf("expected no reclaim while worker running")
	}
}
