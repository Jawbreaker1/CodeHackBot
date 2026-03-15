package cli

import (
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestEnforceRetryModifiedCommandContractRejectsUnchangedAction(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-retry-contract", "", "")

	logPath := filepath.Join(cfg.Session.LogDir, r.sessionID, "logs", "cmd.log")
	r.recordObservationWithCommand("run", "unzip", []string{"-P", "telefo01", "secret.zip"}, logPath, "error", "exit status 1", 1)

	err := r.enforceRetryModifiedCommandContract(assist.Suggestion{
		Type:     "command",
		Decision: "retry_modified",
		Command:  "unzip",
		Args:     []string{"-P", "telefo01", "secret.zip"},
	})
	if err == nil {
		t.Fatalf("expected unchanged retry_modified action to be rejected")
	}
}

func TestEnforceRetryModifiedCommandContractAllowsChangedAction(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-retry-contract-ok", "", "")

	logPath := filepath.Join(cfg.Session.LogDir, r.sessionID, "logs", "cmd.log")
	r.recordObservationWithCommand("run", "unzip", []string{"-P", "telefo01", "secret.zip"}, logPath, "error", "exit status 1", 1)

	err := r.enforceRetryModifiedCommandContract(assist.Suggestion{
		Type:     "command",
		Decision: "retry_modified",
		Command:  "unzip",
		Args:     []string{"-o", "-P", "telefo01", "secret.zip"},
	})
	if err != nil {
		t.Fatalf("expected changed retry_modified action to pass, got %v", err)
	}
}
