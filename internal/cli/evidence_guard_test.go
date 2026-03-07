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

func TestEnforceEvidenceClaimsBlocksUnverifiedVulnerabilityClaim(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-vuln-none", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	out := r.enforceEvidenceClaims("I identified CVE-2010-0738 on the router.")
	if !strings.Contains(out, "source-validated") {
		t.Fatalf("expected vulnerability unverified message, got %q", out)
	}
}

func TestEnforceEvidenceClaimsAllowsVerifiedVulnerabilityClaim(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-vuln-ok", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	manager := r.memoryManager(sessionDir)
	if _, err := manager.RecordObservation(memory.Observation{
		Time:          "now",
		Kind:          "run",
		Command:       "nmap",
		Args:          []string{"--script", "vuln"},
		ExitCode:      0,
		OutputExcerpt: "NSE: host appears vulnerable to CVE-2010-0738",
	}); err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}
	out := r.enforceEvidenceClaims("I identified CVE-2010-0738 on the router.")
	if strings.Contains(out, "UNVERIFIED:") {
		t.Fatalf("did not expect unverified message, got %q", out)
	}
}

func TestEnforceEvidenceClaimsRejectsMismatchedVulnerabilityClaim(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-vuln-mismatch", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	manager := r.memoryManager(sessionDir)
	if _, err := manager.RecordObservation(memory.Observation{
		Time:          "now",
		Kind:          "run",
		Command:       "msfconsole",
		Args:          []string{"-q", "-x", "search cve:2010-0738; exit -y"},
		ExitCode:      0,
		OutputExcerpt: "matching module for CVE-2010-0738",
	}); err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}
	out := r.enforceEvidenceClaims("I identified CVE-2020-9999 on the router.")
	if !strings.Contains(out, "source-validated") {
		t.Fatalf("expected unverified message for mismatch, got %q", out)
	}
}

func TestEnforceEvidenceClaimsLeavesGeneralCVEExplanationUntouched(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-vuln-general", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	text := "CVE identifiers are standardized names used to track known vulnerabilities."
	out := r.enforceEvidenceClaims(text)
	if out != text {
		t.Fatalf("expected untouched explanation, got %q", out)
	}
}
