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
