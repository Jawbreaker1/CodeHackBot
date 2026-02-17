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

func TestRecordObservationUpdatesState(t *testing.T) {
	temp := t.TempDir()
	manager := Manager{
		SessionDir:       temp,
		MaxRecentOutputs: 2,
	}
	state, err := manager.RecordObservation(Observation{
		Time:          "now",
		Kind:          "run",
		Command:       "echo",
		Args:          []string{"hi"},
		ExitCode:      0,
		OutputExcerpt: "hi",
	})
	if err != nil {
		t.Fatalf("RecordObservation error: %v", err)
	}
	if len(state.RecentObservations) != 1 {
		t.Fatalf("recent observations mismatch: %v", state.RecentObservations)
	}
	state, err = manager.RecordObservation(Observation{Time: "later", Kind: "run", Command: "true", ExitCode: 0})
	if err != nil {
		t.Fatalf("RecordObservation error: %v", err)
	}
	state, err = manager.RecordObservation(Observation{Time: "later2", Kind: "run", Command: "false", ExitCode: 1})
	if err != nil {
		t.Fatalf("RecordObservation error: %v", err)
	}
	if len(state.RecentObservations) != 2 {
		t.Fatalf("expected trimming to 2 observations, got %d", len(state.RecentObservations))
	}
	if state.RecentObservations[0].Command != "true" || state.RecentObservations[1].Command != "false" {
		t.Fatalf("unexpected observation order after trim: %+v", state.RecentObservations)
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
	if _, err := manager.RecordObservation(Observation{
		Time:          "now",
		Kind:          "run",
		Command:       "echo",
		Args:          []string{"hi"},
		ExitCode:      0,
		OutputExcerpt: "hi",
	}); err != nil {
		t.Fatalf("RecordObservation error: %v", err)
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
	statePath := filepath.Join(sessionDir, StateFilename)
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(state.RecentLogs) != 0 {
		t.Fatalf("expected recent logs to reset after summarize")
	}
	if len(state.RecentObservations) != 0 {
		t.Fatalf("expected recent observations to reset after summarize")
	}
}
