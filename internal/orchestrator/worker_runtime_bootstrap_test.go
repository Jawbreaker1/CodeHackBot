package orchestrator

import (
	"testing"
	"time"
)

func TestEmitWorkerBootstrapFailureSkipsDuplicateTaskFailure(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-worker-bootstrap-dedupe"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	manager := NewManager(base)
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      "t1",
		WorkerID:    "worker-t1-a1",
		Attempt:     1,
	}
	if err := manager.EmitEvent(runID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFailed, map[string]any{
		"attempt": cfg.Attempt,
		"reason":  WorkerFailureCommandTimeout,
		"error":   "worker command timeout",
	}); err != nil {
		t.Fatalf("EmitEvent task_failed: %v", err)
	}
	if err := EmitWorkerBootstrapFailure(cfg, nil); err != nil {
		t.Fatalf("EmitWorkerBootstrapFailure: %v", err)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	count := 0
	for _, event := range events {
		if event.TaskID == cfg.TaskID && event.WorkerID == WorkerSignalID(cfg.WorkerID) && event.Type == EventTypeTaskFailed {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected single task_failed event, got %d", count)
	}

	// Ensure function still emits when no prior failure exists.
	cfg.TaskID = "t2"
	cfg.Attempt = 2
	if err := EmitWorkerBootstrapFailure(cfg, nil); err != nil {
		t.Fatalf("EmitWorkerBootstrapFailure second task: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	events, err = manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events reload: %v", err)
	}
	count = 0
	for _, event := range events {
		if event.TaskID == "t2" && event.WorkerID == WorkerSignalID(cfg.WorkerID) && event.Type == EventTypeTaskFailed {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected bootstrap failure event for new task, got %d", count)
	}
}
