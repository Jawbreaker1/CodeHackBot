package orchestrator

import (
	"testing"
	"time"
)

func BenchmarkManagerStatusIncrementalGrowth(b *testing.B) {
	base := b.TempDir()
	runID := "run-s28-bench"
	manager := NewManager(base)
	if _, err := EnsureRunLayout(base, runID); err != nil {
		b.Fatalf("EnsureRunLayout: %v", err)
	}
	now := time.Now().UTC()
	for i := 0; i < 3000; i++ {
		if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
			EventID:  NewEventID(),
			RunID:    runID,
			WorkerID: "worker-bench",
			TaskID:   "task-bench",
			Seq:      int64(i + 1),
			TS:       now.Add(time.Duration(i) * time.Millisecond),
			Type:     EventTypeTaskProgress,
			Payload:  mustJSONRaw(map[string]any{"step": i + 1}),
		}); err != nil {
			b.Fatalf("append event: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.Status(runID)
		if err != nil {
			b.Fatalf("Status: %v", err)
		}
	}
}
