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
	templatePath := filepath.Join("internal", "report", "template.md")
	if _, err := os.Stat(templatePath); err != nil {
		templatePath = filepath.Join("..", "..", "internal", "report", "template.md")
	}
	if err := Generate(templatePath, outPath, info); err != nil {
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
	templatePath := filepath.Join("internal", "report", "template.md")
	if _, err := os.Stat(templatePath); err != nil {
		templatePath = filepath.Join("..", "..", "internal", "report", "template.md")
	}
	if err := Generate(templatePath, outPath, info); err != nil {
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
