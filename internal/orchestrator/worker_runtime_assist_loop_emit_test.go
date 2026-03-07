package orchestrator

import (
	"encoding/json"
	"testing"
)

func TestEmitAssistNoNewEvidenceCompletion_LocalWorkflowEmitsObjectiveNotMetFailure(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-assist-no-evidence-objective-not-met"
	taskID := "T-01"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	manager := NewManager(base)
	task := TaskSpec{
		TaskID:            taskID,
		Goal:              "Crack password-protected secret.zip and report password and contents",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"password recovered"},
		FailWhen:          []string{"objective_not_met"},
		ExpectedArtifacts: []string{"recovered_password.txt", "proof_of_access.txt"},
		RiskLevel:         string(RiskReconReadonly),
		Strategy:          "local_archive_recovery",
	}
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    "worker-T-01-a1",
		Attempt:     1,
	}

	err := emitAssistNoNewEvidenceCompletion(
		manager,
		cfg,
		task,
		"test-model",
		"recover",
		3,
		3,
		3,
		"read_file",
		[]string{"secret.zip"},
		3,
		"repeated identical result",
	)
	if err == nil {
		t.Fatalf("expected objective_not_met error")
	}

	events, evErr := manager.Events(runID, 0)
	if evErr != nil {
		t.Fatalf("Events: %v", evErr)
	}
	if hasEventType(events, EventTypeTaskCompleted) {
		t.Fatalf("did not expect task_completed event for objective_not_met path")
	}
	failed, ok := firstEventByType(events, EventTypeTaskFailed)
	if !ok {
		t.Fatalf("expected task_failed event")
	}
	payload := map[string]any{}
	if len(failed.Payload) > 0 {
		_ = json.Unmarshal(failed.Payload, &payload)
	}
	if got := toString(payload["reason"]); got != TaskFailureReasonObjectiveNotMet {
		t.Fatalf("expected reason=%s, got %q", TaskFailureReasonObjectiveNotMet, got)
	}
}
