package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestValidateAssistCompletionContractRequiresFieldsForActionGoal(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-complete-contract", "", "")
	r.startAssistRuntime("extract secret.zip and report password", "execute-step", newAssistBudget("extract secret.zip and report password", 6))
	t.Cleanup(r.clearAssistRuntime)

	if err := r.validateAssistCompletionContract(assist.Suggestion{Type: "complete", Final: "done"}); err == nil {
		t.Fatalf("expected missing completion contract fields to fail")
	}
}

func TestValidateAssistCompletionContractRejectsObjectiveNotMet(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-complete-contract-notmet", "", "")
	r.startAssistRuntime("scan host and produce findings", "execute-step", newAssistBudget("scan host and produce findings", 6))
	t.Cleanup(r.clearAssistRuntime)

	objectiveMet := false
	err := r.validateAssistCompletionContract(assist.Suggestion{
		Type:         "complete",
		ObjectiveMet: &objectiveMet,
		WhyMet:       "scan did not identify target service fingerprint",
	})
	if err == nil {
		t.Fatalf("expected objective_not_met contract failure")
	}
	if !strings.Contains(err.Error(), "objective not met") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAssistCompletionContractPassesWithEvidence(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-complete-contract-pass", "", "")
	r.startAssistRuntime("extract secret.zip and identify password", "execute-step", newAssistBudget("extract secret.zip and identify password", 6))
	t.Cleanup(r.clearAssistRuntime)
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	logPath := filepath.Join(cfg.Session.LogDir, "session-complete-contract-pass", "logs", "cmd.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("john --show secret_zip.hash\nsecret.zip/file.txt:telefo01:file.txt:secret.zip\n"), 0o644); err != nil {
		t.Fatalf("write evidence log: %v", err)
	}
	artifactPath := filepath.Join(cfg.Session.LogDir, "session-complete-contract-pass", "artifacts", "content.txt")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("This is a very very secret file about a cow"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	objectiveMet := true
	err := r.validateAssistCompletionContract(assist.Suggestion{
		Type:         "complete",
		ObjectiveMet: &objectiveMet,
		EvidenceRefs: []string{logPath, artifactPath},
		WhyMet:       "password recovered and decryption verified",
	})
	if err != nil {
		t.Fatalf("expected completion contract to pass: %v", err)
	}
}

func TestValidateAssistCompletionContractRejectsMissingEvidenceRefs(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-complete-contract-missing", "", "")
	r.startAssistRuntime("extract secret.zip and identify password", "execute-step", newAssistBudget("extract secret.zip and identify password", 6))
	t.Cleanup(r.clearAssistRuntime)

	objectiveMet := true
	missingRef := filepath.Join(cfg.Session.LogDir, "session-complete-contract-missing", "logs", "missing.log")
	err := r.validateAssistCompletionContract(assist.Suggestion{
		Type:         "complete",
		ObjectiveMet: &objectiveMet,
		EvidenceRefs: []string{missingRef},
		WhyMet:       "password recovered and decryption verified",
	})
	if err == nil {
		t.Fatalf("expected missing evidence refs to fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "evidence") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAssistCompletionContractSkippedForConversationalGoal(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-complete-contract-chat", "", "")
	r.startAssistRuntime("hello there", "execute-step", newAssistBudget("hello there", 6))
	t.Cleanup(r.clearAssistRuntime)

	if err := r.validateAssistCompletionContract(assist.Suggestion{Type: "complete", Final: "hi"}); err != nil {
		t.Fatalf("expected conversational completion to bypass contract: %v", err)
	}
}

func TestValidateAssistCompletionContractRejectsConversationalReplyForActionGoal(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-complete-contract-greeting", "", "")
	r.startAssistRuntime("crack secret.zip and report the password", "execute-step", newAssistBudget("crack secret.zip and report the password", 6))
	t.Cleanup(r.clearAssistRuntime)
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	logPath := filepath.Join(cfg.Session.LogDir, "session-complete-contract-greeting", "logs", "cmd.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("placeholder evidence"), 0o644); err != nil {
		t.Fatalf("write evidence log: %v", err)
	}
	objectiveMet := true
	err := r.validateAssistCompletionContract(assist.Suggestion{
		Type:         "complete",
		Final:        "Hello! I'm BirdHackBot. What is the scope of this engagement?",
		ObjectiveMet: &objectiveMet,
		EvidenceRefs: []string{logPath},
		WhyMet:       "claimed done",
	})
	if err == nil {
		t.Fatalf("expected conversational completion to fail for action goal")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "conversational reply") {
		t.Fatalf("unexpected error: %v", err)
	}
}
