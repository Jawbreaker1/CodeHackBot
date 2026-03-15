package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteJSONAtomic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "plan.json")
	plan := RunPlan{
		RunID:           "run-1",
		SuccessCriteria: []string{"done"},
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

	if err := WriteJSONAtomic(path, plan); err != nil {
		t.Fatalf("WriteJSONAtomic: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file should not remain: %v", err)
	}
}

func TestReadDedupeAndValidateEvents(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "event.jsonl")
	now := time.Now().UTC()

	events := []EventEnvelope{
		{EventID: "e1", RunID: "r1", WorkerID: "w1", TaskID: "t1", Seq: 1, TS: now, Type: EventTypeTaskStarted},
		{EventID: "e2", RunID: "r1", WorkerID: "w1", TaskID: "t1", Seq: 2, TS: now.Add(time.Second), Type: EventTypeTaskProgress},
		{EventID: "e2", RunID: "r1", WorkerID: "w1", TaskID: "t1", Seq: 3, TS: now.Add(2 * time.Second), Type: EventTypeTaskProgress},
	}
	for _, event := range events {
		if err := AppendEventJSONL(path, event); err != nil {
			t.Fatalf("AppendEventJSONL: %v", err)
		}
	}

	read, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(read) != 3 {
		t.Fatalf("expected 3 events, got %d", len(read))
	}
	if err := ValidateMonotonicSequences(read); err != nil {
		t.Fatalf("ValidateMonotonicSequences: %v", err)
	}

	deduped := DedupeEvents(read)
	if len(deduped) != 2 {
		t.Fatalf("expected 2 deduped events, got %d", len(deduped))
	}
}
