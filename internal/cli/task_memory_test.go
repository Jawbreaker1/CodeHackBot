package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestRecordObservationWritesTaskJournalAndFocus(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-task-journal", "", "")
	r.assistRuntime.Goal = "Crack secret.zip and report password"

	logPath := filepath.Join(cfg.Session.LogDir, r.sessionID, "logs", "cmd.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	r.recordObservationWithCommand("run", "john", []string{"--show", "hash.txt"}, logPath, "1 password hash cracked, 0 left", "", 0)

	sessionDir := filepath.Join(cfg.Session.LogDir, r.sessionID)
	entries, err := readTaskJournalEntries(sessionDir, 5)
	if err != nil {
		t.Fatalf("readTaskJournalEntries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected task journal entry")
	}
	last := entries[len(entries)-1]
	if last.Tool != "john" || last.Result != "ok" {
		t.Fatalf("unexpected journal entry: %+v", last)
	}

	focusPath := filepath.Join(sessionDir, "focus.md")
	focusData, err := os.ReadFile(focusPath)
	if err != nil {
		t.Fatalf("read focus: %v", err)
	}
	focus := string(focusData)
	if !strings.Contains(focus, "Tools tried:") || !strings.Contains(focus, "john(ok:1 fail:0)") {
		t.Fatalf("expected focus tooling summary, got:\n%s", focus)
	}
}

func TestWriteResultSnapshotCreatesArtifact(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-result-snapshot", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	evidencePath := filepath.Join(sessionDir, "logs", "proof.log")
	if err := os.MkdirAll(filepath.Dir(evidencePath), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(evidencePath, []byte("password recovered"), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	trueVal := true
	suggestion := assist.Suggestion{
		Type:         "complete",
		Final:        "Password is telefo01",
		WhyMet:       "Recovered password and extracted file",
		ObjectiveMet: &trueVal,
		EvidenceRefs: []string{evidencePath},
	}
	if err := r.writeResultSnapshot("Crack secret.zip", suggestion); err != nil {
		t.Fatalf("writeResultSnapshot: %v", err)
	}
	data, err := os.ReadFile(resultSnapshotPath(sessionDir))
	if err != nil {
		t.Fatalf("read result snapshot: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "Goal: Crack secret.zip") || !strings.Contains(text, "Password is telefo01") || !strings.Contains(text, evidencePath) {
		t.Fatalf("unexpected result snapshot content:\n%s", text)
	}
}

func TestAssistInputIncludesTaskJournalSummary(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-input-journal", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	if err := r.appendTaskJournalEntry(sessionDir, taskJournalEntry{
		Goal:     "test goal",
		Kind:     "run",
		Tool:     "zip2john",
		Args:     []string{"secret.zip"},
		Result:   "ok",
		Decision: "execute_step",
		LogPath:  "sessions/demo/logs/cmd.log",
	}); err != nil {
		t.Fatalf("appendTaskJournalEntry: %v", err)
	}
	input, err := r.assistInput(sessionDir, "test goal", "execute-step")
	if err != nil {
		t.Fatalf("assistInput: %v", err)
	}
	if !strings.Contains(input.RecentLog, "Task journal:") || !strings.Contains(input.RecentLog, "zip2john") {
		t.Fatalf("expected task journal in recent log, got:\n%s", input.RecentLog)
	}
}
