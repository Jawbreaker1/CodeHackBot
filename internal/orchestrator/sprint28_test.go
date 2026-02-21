package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestManagerEventsMalformedLineQuarantinedAndWarning(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-s28-malformed"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	now := time.Now().UTC()
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e1",
		RunID:    runID,
		WorkerID: orchestratorWorkerID,
		Seq:      1,
		TS:       now,
		Type:     EventTypeRunStarted,
	}); err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	f, err := os.OpenFile(manager.eventPath(runID), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open event log: %v", err)
	}
	if _, err := f.WriteString("{not-json}\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append malformed line: %v", err)
	}
	_ = f.Close()
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e2",
		RunID:    runID,
		WorkerID: "worker-1",
		Seq:      1,
		TS:       now.Add(time.Second),
		Type:     EventTypeWorkerStarted,
	}); err != nil {
		t.Fatalf("append worker_started: %v", err)
	}

	status, err := manager.Status(runID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "running" {
		t.Fatalf("expected running status, got %s", status.State)
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	warnings := 0
	for _, event := range events {
		if event.Type == EventTypeRunWarning {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("expected exactly one warning event, got %d", warnings)
	}
	eventsAgain, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events second read: %v", err)
	}
	warningsAgain := 0
	for _, event := range eventsAgain {
		if event.Type == EventTypeRunWarning {
			warningsAgain++
		}
	}
	if warningsAgain != 1 {
		t.Fatalf("expected deduped warning events, got %d", warningsAgain)
	}

	quarantinePath := manager.quarantinePath(runID)
	data, err := os.ReadFile(quarantinePath)
	if err != nil {
		t.Fatalf("read quarantine file: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected quarantine file to contain malformed line record")
	}
}

func TestManagerSequenceStatePersistsAcrossInstances(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-s28-seq-state"
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	m1 := NewManager(base)
	if err := m1.EmitEvent(runID, "worker-a", "task-1", EventTypeTaskProgress, map[string]any{"message": "one"}); err != nil {
		t.Fatalf("EmitEvent #1: %v", err)
	}
	m2 := NewManager(base)
	if err := m2.EmitEvent(runID, "worker-a", "task-1", EventTypeTaskProgress, map[string]any{"message": "two"}); err != nil {
		t.Fatalf("EmitEvent #2: %v", err)
	}

	events, err := m2.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var seqs []int64
	for _, event := range events {
		if event.WorkerID == "worker-a" {
			seqs = append(seqs, event.Seq)
		}
	}
	if len(seqs) != 2 || seqs[0] != 1 || seqs[1] != 2 {
		t.Fatalf("unexpected worker seqs: %v", seqs)
	}

	stateData, err := os.ReadFile(m2.seqStatePath(runID))
	if err != nil {
		t.Fatalf("read seq state: %v", err)
	}
	var state sequenceState
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatalf("parse seq state: %v", err)
	}
	if got := state.Workers["worker-a"]; got != 2 {
		t.Fatalf("expected persisted seq 2, got %d", got)
	}
}

func TestManagerEmitEventConcurrentWorkersMonotonicSequences(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-s28-concurrent-seq"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	const workerCount = 8
	const eventsPerWorker = 40
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workerID := "worker-" + string(rune('a'+i))
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			for j := 0; j < eventsPerWorker; j++ {
				if err := manager.EmitEvent(runID, workerID, "task-1", EventTypeTaskProgress, map[string]any{
					"step":    j + 1,
					"message": "progress",
				}); err != nil {
					t.Errorf("EmitEvent %s #%d: %v", workerID, j, err)
					return
				}
			}
		}(workerID)
	}
	wg.Wait()

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if err := ValidateMonotonicSequences(events); err != nil {
		t.Fatalf("ValidateMonotonicSequences: %v", err)
	}
	counts := map[string]int{}
	maxSeq := map[string]int64{}
	for _, event := range events {
		if len(event.WorkerID) < len("worker-") || event.WorkerID[:len("worker-")] != "worker-" {
			continue
		}
		counts[event.WorkerID]++
		if event.Seq > maxSeq[event.WorkerID] {
			maxSeq[event.WorkerID] = event.Seq
		}
	}
	if len(counts) != workerCount {
		t.Fatalf("expected %d workers in event stream, got %d", workerCount, len(counts))
	}
	for workerID, count := range counts {
		if count != eventsPerWorker {
			t.Fatalf("worker %s expected %d events, got %d", workerID, eventsPerWorker, count)
		}
		if maxSeq[workerID] != int64(eventsPerWorker) {
			t.Fatalf("worker %s expected max seq %d, got %d", workerID, eventsPerWorker, maxSeq[workerID])
		}
	}
}

func TestIngestEvidenceUsesIncrementalCursor(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-s28-evidence-cursor"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	now := time.Now().UTC()
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-art-1",
		RunID:    runID,
		WorkerID: "worker-1",
		TaskID:   "task-1",
		Seq:      1,
		TS:       now,
		Type:     EventTypeTaskArtifact,
		Payload:  mustJSONRaw(map[string]any{"type": "log", "title": "one", "path": "logs/one.log"}),
	}); err != nil {
		t.Fatalf("append artifact event #1: %v", err)
	}

	first, err := manager.IngestEvidence(runID)
	if err != nil {
		t.Fatalf("first IngestEvidence: %v", err)
	}
	if first.ArtifactsWritten != 1 {
		t.Fatalf("expected first ingest to write one artifact, got %+v", first)
	}

	artifactDir := BuildRunPaths(base, runID).ArtifactDir
	if err := os.Remove(filepath.Join(artifactDir, "e-art-1.json")); err != nil {
		t.Fatalf("remove first artifact file: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-art-2",
		RunID:    runID,
		WorkerID: "worker-1",
		TaskID:   "task-1",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskArtifact,
		Payload:  mustJSONRaw(map[string]any{"type": "log", "title": "two", "path": "logs/two.log"}),
	}); err != nil {
		t.Fatalf("append artifact event #2: %v", err)
	}

	second, err := manager.IngestEvidence(runID)
	if err != nil {
		t.Fatalf("second IngestEvidence: %v", err)
	}
	if second.ArtifactsWritten != 1 {
		t.Fatalf("expected incremental ingest to write only new artifact, got %+v", second)
	}
	if _, err := os.Stat(filepath.Join(artifactDir, "e-art-1.json")); !os.IsNotExist(err) {
		t.Fatalf("expected old artifact not to be reprocessed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(artifactDir, "e-art-2.json")); err != nil {
		t.Fatalf("expected new artifact to be written: %v", err)
	}
}
