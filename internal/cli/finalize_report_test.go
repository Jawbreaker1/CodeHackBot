package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
