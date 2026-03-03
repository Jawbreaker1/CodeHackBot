package orchestrator

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestClassifyWorkerFailureReason(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{reason: WorkerFailureScopeDenied, want: workerFailureClassContract},
		{reason: WorkerFailurePolicyDenied, want: workerFailureClassContract},
		{reason: WorkerFailureAssistLoopDetected, want: workerFailureClassContextLoss},
		{reason: WorkerFailureNoProgress, want: workerFailureClassContextLoss},
		{reason: WorkerFailureCommandFailed, want: workerFailureClassStrategy},
		{reason: WorkerFailureCommandTimeout, want: workerFailureClassStrategy},
	}
	for _, tc := range tests {
		if got := classifyWorkerFailureReason(tc.reason); got != tc.want {
			t.Fatalf("classifyWorkerFailureReason(%q)=%q want %q", tc.reason, got, tc.want)
		}
	}
}

func TestEmitWorkerFailureIncludesFailureClass(t *testing.T) {
	base := t.TempDir()
	manager := NewManager(base)
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-failure-class",
		TaskID:      "T-001",
		WorkerID:    "worker-T-001-a1",
		Attempt:     1,
	}
	task := TaskSpec{
		TaskID:   cfg.TaskID,
		Goal:     "test",
		Targets:  []string{"127.0.0.1"},
		Priority: 1,
	}
	if err := emitWorkerFailure(manager, cfg, task, errors.New("scope denied"), WorkerFailureScopeDenied, nil); err != nil {
		t.Fatalf("emitWorkerFailure: %v", err)
	}
	events, err := manager.Events(cfg.RunID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var failed EventEnvelope
	foundFailed := false
	for _, event := range events {
		if event.Type == EventTypeTaskFailed {
			failed = event
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Fatalf("expected task_failed event")
	}
	failedPayload := map[string]any{}
	if err := json.Unmarshal(failed.Payload, &failedPayload); err != nil {
		t.Fatalf("unmarshal failed payload: %v", err)
	}
	if got, _ := failedPayload["failure_class"].(string); got != workerFailureClassContract {
		t.Fatalf("expected task_failed failure_class=%q, got %q", workerFailureClassContract, got)
	}
	var finding EventEnvelope
	foundFinding := false
	for _, event := range events {
		if event.Type == EventTypeTaskFinding {
			finding = event
			foundFinding = true
			break
		}
	}
	if !foundFinding {
		t.Fatalf("expected task_finding event")
	}
	findingPayload := map[string]any{}
	if err := json.Unmarshal(finding.Payload, &findingPayload); err != nil {
		t.Fatalf("unmarshal finding payload: %v", err)
	}
	meta, _ := findingPayload["metadata"].(map[string]any)
	if got, _ := meta["failure_class"].(string); got != workerFailureClassContract {
		t.Fatalf("expected task_finding metadata.failure_class=%q, got %q", workerFailureClassContract, got)
	}
}
