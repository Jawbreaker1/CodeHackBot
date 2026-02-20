package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateReport(t *testing.T) {
	temp := t.TempDir()
	outPath := filepath.Join(temp, "report.md")
	info := Info{
		Date:      "2026-02-03",
		Scope:     []string{"10.0.0.0/24"},
		Findings:  []string{"Example finding"},
		SessionID: "session-123",
	}
	if err := Generate("", outPath, info); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Date: 2026-02-03") {
		t.Fatalf("expected date replacement")
	}
	if !strings.Contains(content, "Scope: 10.0.0.0/24") {
		t.Fatalf("expected scope replacement")
	}
	if !strings.Contains(content, "High-level findings: Example finding") {
		t.Fatalf("expected findings replacement")
	}
	if !strings.Contains(content, "session-123") {
		t.Fatalf("expected session id in appendix")
	}
}

func TestGenerateReportWithLedger(t *testing.T) {
	temp := t.TempDir()
	outPath := filepath.Join(temp, "report.md")
	info := Info{
		SessionID: "session-456",
		Ledger:    "# Evidence Ledger\n\n| Finding | Command | Log Path | Timestamp | Notes |\n| --- | --- | --- | --- | --- |",
	}
	if err := Generate("", outPath, info); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "## Evidence Ledger") {
		t.Fatalf("expected evidence ledger section")
	}
	if !strings.Contains(content, "Finding | Command") {
		t.Fatalf("expected ledger table")
	}
}

func TestGenerateReportWithContextSections(t *testing.T) {
	temp := t.TempDir()
	outPath := filepath.Join(temp, "report.md")
	info := Info{
		SessionID:    "session-ctx",
		Summary:      "- Summary pending.",
		KnownFacts:   "- host=example\n- status=200",
		Observations: "- ran curl ...",
		Plan:         "1) recon\n2) validate",
		Inventory:    "- nmap\n- curl",
		Focus:        "- create report",
	}
	if err := Generate("", outPath, info); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(data)
	for _, want := range []string{"## Session Summary", "## Known Facts", "## Recent Observations", "## Plan", "## Inventory", "## Task Foundation"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected section %q", want)
		}
	}
}

func TestGenerateWithProfileUsesOWASPTemplate(t *testing.T) {
	temp := t.TempDir()
	outPath := filepath.Join(temp, "owasp.md")
	info := Info{
		Scope:    []string{"10.10.0.0/24"},
		Findings: []string{"Sample finding"},
	}
	if err := GenerateWithProfile("owasp", "", outPath, info); err != nil {
		t.Fatalf("GenerateWithProfile error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# OWASP Security Assessment Report") {
		t.Fatalf("expected OWASP template header")
	}
	if !strings.Contains(content, "## OWASP Methodology Mapping") {
		t.Fatalf("expected OWASP section")
	}
}

func TestGenerateWithProfileRejectsUnknownProfile(t *testing.T) {
	temp := t.TempDir()
	outPath := filepath.Join(temp, "bad.md")
	err := GenerateWithProfile("unknown_profile", "", outPath, Info{})
	if err == nil {
		t.Fatalf("expected unsupported profile error")
	}
	if !strings.Contains(err.Error(), "unsupported report profile") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeProfileAliases(t *testing.T) {
	cases := map[string]string{
		"":         string(ProfileStandard),
		"default":  string(ProfileStandard),
		"OWASP":    string(ProfileOWASP),
		"wstg":     string(ProfileOWASP),
		"n2":       string(ProfileNIS2),
		"internal": string(ProfileInternal),
	}
	for input, want := range cases {
		got, err := NormalizeProfile(input)
		if err != nil {
			t.Fatalf("NormalizeProfile(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeProfile(%q) = %q; want %q", input, got, want)
		}
	}
}

func TestGenerateReportSkipsUnverifiedPrivilegeFact(t *testing.T) {
	temp := t.TempDir()
	outPath := filepath.Join(temp, "report.md")
	info := Info{
		KnownFacts: "- Gained admin access to router without creds",
	}
	if err := Generate("", outPath, info); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(data)
	if strings.Contains(strings.ToLower(content), "gained admin access to router without creds") {
		t.Fatalf("expected unverified privilege fact to be filtered")
	}
}

func TestGenerateReportSkipsUnverifiedPrivilegeFinding(t *testing.T) {
	temp := t.TempDir()
	outPath := filepath.Join(temp, "report.md")
	info := Info{
		Findings: []string{"Confirmed root access on target host"},
	}
	if err := Generate("", outPath, info); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := strings.ToLower(string(data))
	if strings.Contains(content, "confirmed root access on target host") {
		t.Fatalf("expected unverified privilege finding to be filtered")
	}
}
