package cli

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

func TestEnforceEvidenceClaimsBlocksUnverifiedPrivilegeClaim(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-evidence-none", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	out := r.enforceEvidenceClaims("We gained admin access to the router.")
	if !strings.Contains(out, "UNVERIFIED:") {
		t.Fatalf("expected unverified message, got %q", out)
	}
}

func TestEnforceEvidenceClaimsAllowsVerifiedPrivilegeClaim(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-evidence-ok", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	manager := r.memoryManager(sessionDir)
	if _, err := manager.RecordObservation(memory.Observation{
		Time:          "now",
		Kind:          "run",
		Command:       "id",
		ExitCode:      0,
		OutputExcerpt: "uid=0(root) gid=0(root)",
	}); err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}
	out := r.enforceEvidenceClaims("We gained admin access to the router.")
	if strings.Contains(out, "UNVERIFIED:") {
		t.Fatalf("did not expect unverified message, got %q", out)
	}
}
