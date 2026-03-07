package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestParseGoalEvalResponseExtractsWrappedJSON(t *testing.T) {
	raw := "analysis...\n{\"done\":true,\"answer\":\"ok\",\"confidence\":\"high\"}\n"
	got, err := parseGoalEvalResponse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !got.Done {
		t.Fatalf("expected done=true")
	}
	if got.Answer != "ok" {
		t.Fatalf("unexpected answer: %q", got.Answer)
	}
}

func TestParseGoalEvalResponseNormalizesConfidence(t *testing.T) {
	raw := "{\"done\":false,\"answer\":\"\",\"confidence\":\"VERY\"}"
	got, err := parseGoalEvalResponse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got.Confidence != "low" {
		t.Fatalf("expected low confidence fallback, got %q", got.Confidence)
	}
}

func TestShouldAttemptGoalEvaluationSkipsWriteCreationGoals(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-goal-eval", "", "")
	r.lastActionLogPath = filepath.Join(cfg.Session.LogDir, "demo.log")

	if r.shouldAttemptGoalEvaluation("create a syve.md report in owasp format") {
		t.Fatalf("expected goal evaluation to be skipped for write/create goals")
	}
}

func TestCollectGoalEvalArtifactsIncludesPrimaryAndRecentLogs(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-goal-eval", "", "")
	sessionLogs := filepath.Join(cfg.Session.LogDir, r.sessionID, "logs")
	if err := os.MkdirAll(sessionLogs, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}

	primaryPath := filepath.Join(cfg.Session.LogDir, "primary.log")
	if err := os.WriteFile(primaryPath, []byte("primary evidence"), 0o644); err != nil {
		t.Fatalf("write primary: %v", err)
	}
	oldPath := filepath.Join(sessionLogs, "old.log")
	newPath := filepath.Join(sessionLogs, "new.log")
	if err := os.WriteFile(oldPath, []byte("old evidence"), 0o644); err != nil {
		t.Fatalf("write old log: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new evidence"), 0o644); err != nil {
		t.Fatalf("write new log: %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(oldPath, now.Add(-2*time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("set old mtime: %v", err)
	}
	if err := os.Chtimes(newPath, now.Add(-1*time.Minute), now.Add(-1*time.Minute)); err != nil {
		t.Fatalf("set new mtime: %v", err)
	}

	artifacts := r.collectGoalEvalArtifacts(primaryPath)
	if len(artifacts) < 3 {
		t.Fatalf("expected at least 3 artifacts, got %d", len(artifacts))
	}
	if artifacts[0].Path != primaryPath {
		t.Fatalf("expected primary path first, got %s", artifacts[0].Path)
	}
	if artifacts[1].Path != newPath {
		t.Fatalf("expected newest log second, got %s", artifacts[1].Path)
	}
	if artifacts[2].Path != oldPath {
		t.Fatalf("expected older log third, got %s", artifacts[2].Path)
	}
}

func TestRecordBridgedCompletionWritesSnapshotAndJournal(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-goal-eval-bridge", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	logDir := filepath.Join(sessionDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	logPath := filepath.Join(logDir, "cmd-demo.log")
	if err := os.WriteFile(logPath, []byte("proof line"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	r.lastActionLogPath = logPath

	if err := r.recordBridgedCompletion(
		"Crack secret.zip and report password",
		"Password: telefo01",
		goalEvalResult{Done: true, Reason: "verified from evidence"},
	); err != nil {
		t.Fatalf("recordBridgedCompletion: %v", err)
	}

	snapshotPath := resultSnapshotPath(sessionDir)
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	text := string(data)
	if !containsAll(text, "Objective status: met", "Password: telefo01", "verified from evidence") {
		t.Fatalf("unexpected snapshot content:\n%s", text)
	}
	if !containsAll(text, logPath) {
		t.Fatalf("expected snapshot evidence ref: %s", logPath)
	}

	entries, err := readTaskJournalEntries(sessionDir, 5)
	if err != nil {
		t.Fatalf("readTaskJournalEntries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected task journal entries")
	}
	last := entries[len(entries)-1]
	if last.Kind != "complete" || last.Result != "ok" {
		t.Fatalf("unexpected journal completion entry: %+v", last)
	}
}
