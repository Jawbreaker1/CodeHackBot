package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Info struct {
	Date      string
	Scope     []string
	Findings  []string
	SessionID string
	Ledger    string
}

func DefaultTemplatePath() string {
	return filepath.Join("internal", "report", "template.md")
}

func Generate(templatePath, outPath string, info Info) error {
	if templatePath == "" {
		templatePath = DefaultTemplatePath()
	}
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read template: %w", err)
	}
	content := string(data)
	date := info.Date
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	content = strings.ReplaceAll(content, "Date:", fmt.Sprintf("Date: %s", date))
	if len(info.Scope) > 0 {
		content = strings.ReplaceAll(content, "Scope:", fmt.Sprintf("Scope: %s", strings.Join(info.Scope, ", ")))
	}
	if len(info.Findings) > 0 {
		content = strings.ReplaceAll(content, "High-level findings:", fmt.Sprintf("High-level findings: %s", strings.Join(info.Findings, "; ")))
	}
	if info.SessionID != "" {
		content = strings.ReplaceAll(content, "Session IDs", fmt.Sprintf("Session IDs\n- %s", info.SessionID))
	}
	if info.Ledger != "" {
		content = strings.TrimSpace(content) + "\n\n## Evidence Ledger\n\n" + strings.TrimSpace(info.Ledger) + "\n"
	}
	if outPath == "" {
		return fmt.Errorf("output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
