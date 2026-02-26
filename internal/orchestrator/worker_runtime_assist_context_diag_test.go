package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizePathAnchorToken(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "/tmp/secret.zip", want: "/tmp/secret.zip"},
		{in: "zip.hash", want: "zip.hash"},
		{in: "-la", want: ""},
		{in: "localhost", want: ""},
	}
	for _, tc := range tests {
		if got := normalizePathAnchorToken(tc.in); got != tc.want {
			t.Fatalf("normalizePathAnchorToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSummarizeOutputWithMetaTracksTruncation(t *testing.T) {
	out := make([]byte, 900)
	for i := range out {
		out[i] = 'a'
	}
	summary, meta := summarizeOutputWithMeta(out)
	if summary == "" {
		t.Fatalf("expected non-empty summary")
	}
	if !meta.OutputByteCapped {
		t.Fatalf("expected OutputByteCapped true")
	}
	if !meta.OutputCharCapped {
		t.Fatalf("expected OutputCharCapped true")
	}
}

func TestWriteWorkerAssistContextEnvelope(t *testing.T) {
	base := t.TempDir()
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-context-env",
		TaskID:      "T-001",
		WorkerID:    "worker-T-001-a1",
		Attempt:     1,
	}
	envelope := newWorkerAssistContextEnvelope(cfg, TaskSpec{TaskID: "T-001"}, "test goal")
	envelope.recordPrompt("execute-step", "", "recent", "chat")
	envelope.recordProgress(1, 1, 1, 2)
	envelope.recordCommandSummary(workerAssistCommandSummaryMeta{OutputByteCapped: true})
	envelope.recordCommandOutcome("list_dir", []string{"-la", "/tmp"}, nil, "result-key")

	now := func() time.Time { return time.Date(2026, 2, 26, 12, 0, 0, 0, time.UTC) }
	if err := writeWorkerAssistContextEnvelope(cfg, "T-001", envelope, now); err != nil {
		t.Fatalf("writeWorkerAssistContextEnvelope: %v", err)
	}
	path := filepath.Join(BuildRunPaths(base, cfg.RunID).ArtifactDir, "T-001", "context_envelope.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected context_envelope.json: %v", err)
	}
	attemptPath := filepath.Join(BuildRunPaths(base, cfg.RunID).ArtifactDir, "T-001", "context_envelope.a1.json")
	if _, err := os.Stat(attemptPath); err != nil {
		t.Fatalf("expected context_envelope.a1.json: %v", err)
	}
}

func TestLoadPreviousWorkerAssistContextEnvelope(t *testing.T) {
	base := t.TempDir()
	runID := "run-context-carryover"
	taskID := "T-001"
	cfgAttempt1 := WorkerRunConfig{
		SessionsDir: base,
		RunID:       runID,
		TaskID:      taskID,
		WorkerID:    "worker-T-001-a1",
		Attempt:     1,
	}
	env := newWorkerAssistContextEnvelope(cfgAttempt1, TaskSpec{TaskID: taskID}, "goal")
	env.Observations.RetainedTail = []string{"command ok: list_dir /tmp"}
	env.Anchors.LastFailure = "exit status 1"
	if err := writeWorkerAssistContextEnvelope(cfgAttempt1, taskID, env, time.Now); err != nil {
		t.Fatalf("writeWorkerAssistContextEnvelope attempt1: %v", err)
	}

	cfgAttempt2 := cfgAttempt1
	cfgAttempt2.Attempt = 2
	prior, err := loadPreviousWorkerAssistContextEnvelope(cfgAttempt2, taskID)
	if err != nil {
		t.Fatalf("loadPreviousWorkerAssistContextEnvelope: %v", err)
	}
	if prior == nil {
		t.Fatalf("expected prior envelope")
	}
	if prior.Attempt != 1 {
		t.Fatalf("expected attempt=1, got %d", prior.Attempt)
	}
	if len(prior.Observations.RetainedTail) != 1 {
		t.Fatalf("expected retained tail entry, got %d", len(prior.Observations.RetainedTail))
	}
}
