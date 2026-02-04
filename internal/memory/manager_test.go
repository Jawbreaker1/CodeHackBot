package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordLogUpdatesState(t *testing.T) {
	temp := t.TempDir()
	manager := Manager{
		SessionDir:       temp,
		MaxRecentOutputs: 2,
	}
	logPath := filepath.Join(temp, "logs", "cmd-1.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("output"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	state, err := manager.RecordLog(logPath)
	if err != nil {
		t.Fatalf("RecordLog error: %v", err)
	}
	if state.StepsSinceSummary != 1 {
		t.Fatalf("steps mismatch: %d", state.StepsSinceSummary)
	}
	if len(state.RecentLogs) != 1 {
		t.Fatalf("recent logs mismatch: %v", state.RecentLogs)
	}

	logPath2 := filepath.Join(temp, "logs", "cmd-2.log")
	if err := os.WriteFile(logPath2, []byte("output2"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	state, err = manager.RecordLog(logPath2)
	if err != nil {
		t.Fatalf("RecordLog error: %v", err)
	}
	if len(state.RecentLogs) != 2 {
		t.Fatalf("recent logs length mismatch: %v", state.RecentLogs)
	}
}

func TestShouldSummarize(t *testing.T) {
	manager := Manager{
		MaxRecentOutputs:   10,
		SummarizeEvery:     3,
		SummarizeAtPercent: 70,
	}
	state := State{StepsSinceSummary: 2, RecentLogs: []string{"a", "b", "c", "d", "e", "f", "g"}}
	if !manager.ShouldSummarize(state) {
		t.Fatalf("expected summarize true via percent threshold")
	}
	state = State{StepsSinceSummary: 3, RecentLogs: []string{"a"}}
	if !manager.ShouldSummarize(state) {
		t.Fatalf("expected summarize true via step threshold")
	}
	state = State{StepsSinceSummary: 1, RecentLogs: []string{"a"}}
	if manager.ShouldSummarize(state) {
		t.Fatalf("expected summarize false")
	}
}

func TestSummarizeFallback(t *testing.T) {
	temp := t.TempDir()
	sessionDir := filepath.Join(temp, "session-1")
	logDir := filepath.Join(sessionDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	logPath := filepath.Join(logDir, "cmd-1.log")
	if err := os.WriteFile(logPath, []byte("output"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	manager := Manager{
		SessionDir:       sessionDir,
		LogDir:           logDir,
		MaxRecentOutputs: 5,
	}
	if _, err := manager.RecordLog(logPath); err != nil {
		t.Fatalf("RecordLog error: %v", err)
	}
	if err := manager.Summarize(context.Background(), FallbackSummarizer{}, "manual"); err != nil {
		t.Fatalf("Summarize error: %v", err)
	}
	summaryPath := filepath.Join(sessionDir, SummaryFilename)
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !strings.Contains(string(data), "Summary") {
		t.Fatalf("expected summary content")
	}
}
