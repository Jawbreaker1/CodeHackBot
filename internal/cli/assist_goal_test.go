package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestEnrichAssistGoalSkipsExecuteStepMode(t *testing.T) {
	cfg := config.Config{}
	runner := NewRunner(cfg, "session-1", "", "")

	logPath := filepath.Join(t.TempDir(), "cmd.log")
	if err := os.WriteFile(logPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	runner.lastActionLogPath = logPath

	goal := "dig +short systemverification.com"
	got := runner.enrichAssistGoal(goal, "execute-step")
	if got != goal {
		t.Fatalf("execute-step goal should not be enriched; got %q", got)
	}
}

func TestEnrichAssistGoalIncludesArtifactForRecoveryModes(t *testing.T) {
	cfg := config.Config{}
	runner := NewRunner(cfg, "session-1", "", "")

	logPath := filepath.Join(t.TempDir(), "cmd.log")
	if err := os.WriteFile(logPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	runner.lastActionLogPath = logPath

	got := runner.enrichAssistGoal("recover the failed step", "recover")
	if !strings.Contains(got, "latest action artifact") {
		t.Fatalf("expected recovery goal to include artifact context: %q", got)
	}
	if !strings.Contains(got, logPath) {
		t.Fatalf("expected recovery goal to include log path: %q", got)
	}
}

func TestRecoveryDirectiveForWriteGoalAfterListDir(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	runner := NewRunner(cfg, "session-recover-write", "", "")
	runner.recordObservationWithCommand("list_dir", "list_dir", []string{"."}, "", "entries=10", "", 0)

	got := runner.recoveryDirectiveForGoal("create syve.md report in owasp format", "recover")
	if !strings.Contains(got, "create/write action") {
		t.Fatalf("expected write-action recovery directive, got %q", got)
	}
}

func TestEnrichAssistGoalIncludesRepeatedCommandDirective(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	runner := NewRunner(cfg, "session-repeat-directive", "", "")
	runner.lastAssistCmdKey = "unzip -l secret.zip"

	got := runner.enrichAssistGoal("continue cracking secret.zip", "recover")
	if !strings.Contains(got, "blocked as repeated") {
		t.Fatalf("expected repeated-command directive, got %q", got)
	}
	if !strings.Contains(got, "unzip -l secret.zip") {
		t.Fatalf("expected repeated command details, got %q", got)
	}
}

func TestShouldTreatPendingInputAsNewGoal(t *testing.T) {
	if !shouldTreatPendingInputAsNewGoal("are there any playbooks you can look at?", "Share the exact next action/target") {
		t.Fatalf("expected meta question to be treated as a new goal")
	}
	if shouldTreatPendingInputAsNewGoal("yes", "Share the exact next action/target") {
		t.Fatalf("expected short confirmation to stay as follow-up answer")
	}
	if shouldTreatPendingInputAsNewGoal("go ahead with default", "Should I proceed with a default wordlist?") {
		t.Fatalf("expected default-choice answer to stay as follow-up")
	}
}

func TestAutoAssistFollowUpAnswerAuthorizationConfirm(t *testing.T) {
	answer, ok := autoAssistFollowUpAnswer("Is this correct and authorized within your lab environment?")
	if !ok {
		t.Fatalf("expected auto answer for authorization confirmation")
	}
	if !strings.Contains(strings.ToLower(answer), "proceed") {
		t.Fatalf("unexpected auto answer: %q", answer)
	}
}
