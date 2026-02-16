package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestHandleSummarizeReadonly(t *testing.T) {
	cfg := config.Config{}
	cfg.Permissions.Level = "readonly"
	cfg.Session.LogDir = t.TempDir()
	cfg.LLM.BaseURL = "localhost:1234/v1"
	runner := NewRunner(cfg, "session-1", "", "")
	if err := runner.handleSummarize(nil); err == nil {
		t.Fatalf("expected readonly error")
	}
}

func TestSummaryArtifactPathPrefersLastSuccessfulForNormalSummary(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	runner := NewRunner(cfg, "session-sum-success", "", "")
	sessionDir, err := runner.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}

	successLog := filepath.Join(sessionDir, "logs", "success.log")
	failedLog := filepath.Join(sessionDir, "logs", "failed.log")
	if err := os.WriteFile(successLog, []byte("password found"), 0o644); err != nil {
		t.Fatalf("write success log: %v", err)
	}
	if err := os.WriteFile(failedLog, []byte("password not found"), 0o644); err != nil {
		t.Fatalf("write failed log: %v", err)
	}

	runner.recordActionArtifact(successLog)
	runner.recordObservationWithCommand("run", "fcrackzip", []string{"secret.zip"}, successLog, "password found", "", 0)
	runner.recordActionArtifact(failedLog)
	runner.recordObservationWithCommand("run", "john", []string{"secret.zip"}, failedLog, "no passwords cracked", "exit status 1", 1)

	got := runner.summaryArtifactPath("summarize how you cracked secret.zip")
	if got != successLog {
		t.Fatalf("expected success log for summary, got %q", got)
	}
}

func TestSummaryArtifactPathPrefersLatestFailureForFailureIntent(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	runner := NewRunner(cfg, "session-sum-failure", "", "")
	sessionDir, err := runner.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}

	successLog := filepath.Join(sessionDir, "logs", "success.log")
	failedLog := filepath.Join(sessionDir, "logs", "failed.log")
	if err := os.WriteFile(successLog, []byte("password found"), 0o644); err != nil {
		t.Fatalf("write success log: %v", err)
	}
	if err := os.WriteFile(failedLog, []byte("password not found"), 0o644); err != nil {
		t.Fatalf("write failed log: %v", err)
	}

	runner.recordActionArtifact(successLog)
	runner.recordObservationWithCommand("run", "fcrackzip", []string{"secret.zip"}, successLog, "password found", "", 0)
	runner.recordActionArtifact(failedLog)
	runner.recordObservationWithCommand("run", "john", []string{"secret.zip"}, failedLog, "no passwords cracked", "exit status 1", 1)

	got := runner.summaryArtifactPath("why did this fail?")
	if got != failedLog {
		t.Fatalf("expected failed log for failure intent, got %q", got)
	}
}
