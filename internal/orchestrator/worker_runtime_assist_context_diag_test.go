package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
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

func TestWorkerAssistContextEnvelopeObservationAppendDiagnostics(t *testing.T) {
	env := newWorkerAssistContextEnvelope(WorkerRunConfig{}, TaskSpec{}, "goal")
	before := []string{"obs one", "obs two"}
	after := []string{"obs two"}
	env.recordObservationAppend(before, after)
	if env.Observations.AppendCalls != 1 {
		t.Fatalf("expected append_calls=1, got %d", env.Observations.AppendCalls)
	}
	if env.Observations.EvictionEvents != 1 {
		t.Fatalf("expected eviction event, got %d", env.Observations.EvictionEvents)
	}
	if env.Observations.TokenBudgetCompactionEvents != 1 {
		t.Fatalf("expected token compaction event, got %d", env.Observations.TokenBudgetCompactionEvents)
	}
	before = []string{"obs two"}
	after = []string{"compaction_summary: dropped=3 | paths=/tmp/a.log", "obs two"}
	env.recordObservationAppend(before, after)
	if env.Observations.CompactionSummaryEvents != 1 {
		t.Fatalf("expected compaction summary event, got %d", env.Observations.CompactionSummaryEvents)
	}
}

func TestWorkerAssistContextEnvelopeRetryResetAndAttemptDelta(t *testing.T) {
	cfg := WorkerRunConfig{RunID: "run", TaskID: "T-001", WorkerID: "worker", Attempt: 2}
	prior := newWorkerAssistContextEnvelope(cfg, TaskSpec{TaskID: "T-001"}, "goal")
	prior.Attempt = 1
	prior.Observations.CurrentCount = 3
	prior.Anchors.RetainedPathAnchors = []string{"/tmp/secret.zip"}
	prior.Anchors.LastFailure = "exit status 1"
	prior.Anchors.LastResultFingerprint = "result:abc"
	prior.Truncation.OutputByteCapEvents = 1
	prior.Fingerprints.ActionRepeatEvents = 2
	prior.Fingerprints.ResultRepeatEvents = 1

	current := newWorkerAssistContextEnvelope(cfg, TaskSpec{TaskID: "T-001"}, "goal")
	current.applyCarryover(prior)
	current.recordCarryoverObservations(0)
	current.recordCarryoverRecoverySignals(false, false)
	current.Observations.CurrentCount = 5
	current.Truncation.OutputByteCapEvents = 2
	current.Fingerprints.ActionRepeatEvents = 4
	current.Fingerprints.ResultRepeatEvents = 2

	current.finalizeRetryResetSignals(prior)
	if len(current.Retry.ResetSignals) == 0 {
		t.Fatalf("expected retry reset signals")
	}
	summary := current.finalizeAttemptDelta(prior)
	if summary == "" {
		t.Fatalf("expected attempt delta summary")
	}
	if current.AttemptDelta.ComparedToAttempt != 1 {
		t.Fatalf("expected compared attempt=1, got %d", current.AttemptDelta.ComparedToAttempt)
	}
	if !strings.Contains(summary, "observations=3->5") {
		t.Fatalf("expected observation delta in summary, got %q", summary)
	}
}

type scriptedAssistTurns struct {
	mu    sync.Mutex
	seq   []assist.Suggestion
	index int
}

func (s *scriptedAssistTurns) Suggest(_ context.Context, _ assist.Input) (assist.Suggestion, workerAssistantTurnMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seq) == 0 {
		return assist.Suggestion{Type: "complete", Final: "done"}, workerAssistantTurnMeta{}, nil
	}
	if s.index >= len(s.seq) {
		last := s.seq[len(s.seq)-1]
		return last, workerAssistantTurnMeta{}, nil
	}
	out := s.seq[s.index]
	s.index++
	return out, workerAssistantTurnMeta{}, nil
}

func TestRunWorkerTaskAssistContextEnvelopeCapturesCarryoverAndDelta(t *testing.T) {
	base := t.TempDir()
	runID := "run-assist-context-carryover"
	taskID := "T-CTX"
	workerID := "worker-T-CTX"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	writeWorkerPlan(t, base, runID, Scope{Targets: []string{"127.0.0.1"}})
	writeAssistTask(t, base, runID, TaskSpec{
		TaskID:            taskID,
		Goal:              "inspect local files",
		Targets:           []string{"127.0.0.1"},
		DoneWhen:          []string{"task complete"},
		FailWhen:          []string{"task failed"},
		ExpectedArtifacts: []string{"assist logs"},
		RiskLevel:         string(RiskReconReadonly),
		Action: TaskAction{
			Type:   "assist",
			Prompt: "Inspect /tmp and summarize findings.",
		},
		Budget: TaskBudget{
			MaxSteps:     4,
			MaxToolCalls: 4,
			MaxRuntime:   10 * time.Second,
		},
	})

	runAttempt := func(attempt int) {
		assistant := &scriptedAssistTurns{
			seq: []assist.Suggestion{
				{Type: "command", Command: "list_dir", Args: []string{"-la", "/tmp"}, Summary: "unknown under test: enumerate local /tmp entries"},
				{Type: "complete", Final: "done"},
			},
		}
		cfg := WorkerRunConfig{
			SessionsDir: base,
			RunID:       runID,
			TaskID:      taskID,
			WorkerID:    workerID,
			Attempt:     attempt,
			assistantBuilder: func() (string, string, workerAssistant, error) {
				return "test-model", "strict", assistant, nil
			},
		}
		if err := RunWorkerTask(cfg); err != nil {
			t.Fatalf("RunWorkerTask attempt %d: %v", attempt, err)
		}
	}

	runAttempt(1)
	runAttempt(2)

	envPath := filepath.Join(BuildRunPaths(base, runID).ArtifactDir, taskID, "context_envelope.json")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read context_envelope: %v", err)
	}
	var env workerAssistContextEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal context_envelope: %v", err)
	}
	if env.Attempt != 2 {
		t.Fatalf("expected latest attempt 2, got %d", env.Attempt)
	}
	if env.Retry.PriorAttempt != 1 {
		t.Fatalf("expected prior attempt 1, got %d", env.Retry.PriorAttempt)
	}
	if env.Retry.CarryoverObservationCount == 0 {
		t.Fatalf("expected carryover observations to be recorded")
	}
	if env.AttemptDelta.ComparedToAttempt != 1 {
		t.Fatalf("expected attempt delta compared_to_attempt=1, got %d", env.AttemptDelta.ComparedToAttempt)
	}
	if len(env.AttemptDelta.Summary) == 0 {
		t.Fatalf("expected non-empty attempt delta summary")
	}
	if env.Observations.AppendCalls == 0 {
		t.Fatalf("expected observation append telemetry")
	}
	foundCarryover := false
	for _, entry := range env.Observations.RetainedTail {
		if strings.Contains(entry, "carryover:") {
			foundCarryover = true
			break
		}
	}
	if !foundCarryover {
		t.Fatalf("expected carryover entry in retained tail")
	}

	events, err := NewManager(base).Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	foundDeltaEvent := false
	for _, event := range events {
		if event.Type != EventTypeTaskProgress {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if _, ok := payload["attempt_delta_summary"].(string); ok {
			foundDeltaEvent = true
			break
		}
	}
	if !foundDeltaEvent {
		t.Fatalf("expected attempt_delta_summary progress event")
	}
}
