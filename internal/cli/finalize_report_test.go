package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

func TestFinalizeReportWritesArtifact(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Session.PlanFilename = "plan.md"
	cfg.Session.InventoryFilename = "inventory.md"
	cfg.Session.LedgerFilename = "ledger.md"
	cfg.Context.MaxRecentOutputs = 10

	r := NewRunner(cfg, "session-1", "", "")
	r.lastKnownTarget = "example.com"

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		t.Fatalf("EnsureArtifacts: %v", err)
	}
	if err := os.WriteFile(artifacts.SummaryPath, []byte("# Session Summary\n\n- ok\n"), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	if err := os.WriteFile(artifacts.FactsPath, []byte("# Known Facts\n\n- status=200\n"), 0o644); err != nil {
		t.Fatalf("write facts: %v", err)
	}
	manager := r.memoryManager(sessionDir)
	if _, err := manager.RecordObservation(memory.Observation{
		Time:          "now",
		Kind:          "browse",
		Command:       "browse",
		Args:          []string{"https://example.com"},
		ExitCode:      0,
		OutputExcerpt: "Status: 200",
		LogPath:       filepath.Join(sessionDir, "logs", "browse.log"),
	}); err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}

	outPath, err := r.finalizeReport("create a security report")
	if err != nil {
		t.Fatalf("finalizeReport: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Security Assessment Report") {
		t.Fatalf("missing template header")
	}
	if !strings.Contains(content, "## Session Summary") || !strings.Contains(content, "## Recent Observations") {
		t.Fatalf("missing context sections")
	}
}

func TestFinalizeReportUsesRequestedFilenameFromGoal(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Session.PlanFilename = "plan.md"
	cfg.Session.InventoryFilename = "inventory.md"
	cfg.Session.LedgerFilename = "ledger.md"
	cfg.Context.MaxRecentOutputs = 10
	cfg.Permissions.Level = "all"

	r := NewRunner(cfg, "session-2", "", "")
	r.lastKnownTarget = "example.com"

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		t.Fatalf("EnsureArtifacts: %v", err)
	}
	if err := os.WriteFile(artifacts.SummaryPath, []byte("# Session Summary\n\n- ok\n"), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	wd := t.TempDir()
	origWD, _ := os.Getwd()
	_ = os.Chdir(wd)
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	outPath, err := r.finalizeReport("scan target and create security_report.md in this folder")
	if err != nil {
		t.Fatalf("finalizeReport: %v", err)
	}
	wantPath := filepath.Join(wd, "security_report.md")
	realOut, err := filepath.EvalSymlinks(outPath)
	if err != nil {
		t.Fatalf("eval out path: %v", err)
	}
	realWant, err := filepath.EvalSymlinks(wantPath)
	if err != nil {
		t.Fatalf("eval want path: %v", err)
	}
	if realOut != realWant {
		t.Fatalf("unexpected output path: got %s want %s", realOut, realWant)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected report at requested path: %v", err)
	}
}

func TestExecuteAssistSuggestionReportCommand(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Permissions.Level = "all"
	r := NewRunner(cfg, "session-report-cmd", "", "")

	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	wd := t.TempDir()
	origWD, _ := os.Getwd()
	_ = os.Chdir(wd)
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	if err := r.executeAssistSuggestion(assist.Suggestion{
		Type:    "command",
		Command: "report",
		Args:    []string{"security_report.md"},
	}, false); err != nil {
		t.Fatalf("executeAssistSuggestion(report): %v", err)
	}
	if _, err := os.Stat(filepath.Join(wd, "security_report.md")); err != nil {
		t.Fatalf("expected security_report.md to be created: %v", err)
	}
}

func TestExecuteAssistSuggestionReportCommandWithProfile(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Permissions.Level = "all"
	r := NewRunner(cfg, "session-report-profile-cmd", "", "")

	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	wd := t.TempDir()
	origWD, _ := os.Getwd()
	_ = os.Chdir(wd)
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	if err := r.executeAssistSuggestion(assist.Suggestion{
		Type:    "command",
		Command: "report",
		Args:    []string{"owasp", "security_report.md"},
	}, false); err != nil {
		t.Fatalf("executeAssistSuggestion(report owasp): %v", err)
	}
	path := filepath.Join(wd, "security_report.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(data), "# OWASP Security Assessment Report") {
		t.Fatalf("expected OWASP template in report output")
	}
}

func TestFinalizeReportSelectsProfileFromGoal(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Session.PlanFilename = "plan.md"
	cfg.Session.InventoryFilename = "inventory.md"
	cfg.Session.LedgerFilename = "ledger.md"
	cfg.Context.MaxRecentOutputs = 10
	cfg.Permissions.Level = "all"

	r := NewRunner(cfg, "session-profile-goal", "", "")
	r.lastKnownTarget = "example.com"

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensureSessionScaffold: %v", err)
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		t.Fatalf("EnsureArtifacts: %v", err)
	}
	if err := os.WriteFile(artifacts.SummaryPath, []byte("# Session Summary\n\n- ok\n"), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	outPath, err := r.finalizeReport("create an OWASP security report")
	if err != nil {
		t.Fatalf("finalizeReport: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(data), "# OWASP Security Assessment Report") {
		t.Fatalf("expected OWASP profile report")
	}
}

func TestParseReportArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantP   string
		wantOut string
		wantErr bool
	}{
		{name: "default", args: nil, wantP: "standard"},
		{name: "profile only", args: []string{"owasp"}, wantP: "owasp"},
		{name: "output only", args: []string{"security_report.md"}, wantP: "standard", wantOut: "security_report.md"},
		{name: "profile and output", args: []string{"nis2", "security_report.md"}, wantP: "nis2", wantOut: "security_report.md"},
		{name: "invalid extra args", args: []string{"owasp", "a.md", "b.md"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotP, gotOut, err := parseReportArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseReportArgs error: %v", err)
			}
			if gotP != tc.wantP {
				t.Fatalf("profile mismatch: got %q want %q", gotP, tc.wantP)
			}
			if gotOut != tc.wantOut {
				t.Fatalf("output mismatch: got %q want %q", gotOut, tc.wantOut)
			}
		})
	}
}
