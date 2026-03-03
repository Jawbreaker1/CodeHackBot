package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

func TestRefreshAssistContextSummarySnapshotUpdatesSummaryAndPreservesState(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Context.MaxRecentOutputs = 20
	cfg.Context.ChatHistoryLines = 20
	r := NewRunner(cfg, "session-summary-refresh", "", "")

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	logPath := filepath.Join(sessionDir, "logs", "cmd-demo.log")
	logBody := "$ nmap -sn 192.168.50.1\nNmap scan report for 192.168.50.1\nHost is up.\n"
	if err := os.WriteFile(logPath, []byte(logBody), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	manager := r.memoryManager(sessionDir)
	if _, err := manager.RecordLog(logPath); err != nil {
		t.Fatalf("RecordLog: %v", err)
	}
	r.recordObservationWithCommand("run", "nmap", []string{"-sn", "192.168.50.1"}, logPath, "Host is up", "", 0)

	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		t.Fatalf("EnsureArtifacts: %v", err)
	}
	before, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		t.Fatalf("LoadState before: %v", err)
	}
	if before.StepsSinceSummary <= 0 {
		t.Fatalf("expected pending summary steps, got %d", before.StepsSinceSummary)
	}

	if err := r.refreshAssistContextSummarySnapshot(sessionDir, "execute-step"); err != nil {
		t.Fatalf("refreshAssistContextSummarySnapshot: %v", err)
	}

	summaryData, err := os.ReadFile(artifacts.SummaryPath)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	summaryText := string(summaryData)
	if strings.Contains(summaryText, "Summary pending.") {
		t.Fatalf("expected summary refresh to replace pending marker, got:\n%s", summaryText)
	}
	if !strings.Contains(summaryText, "Recent actions:") {
		t.Fatalf("expected refreshed action summary, got:\n%s", summaryText)
	}

	factsData, err := os.ReadFile(artifacts.FactsPath)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if !strings.Contains(string(factsData), "Observed IP: 192.168.50.1") {
		t.Fatalf("expected observed IP fact, got:\n%s", string(factsData))
	}

	after, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		t.Fatalf("LoadState after: %v", err)
	}
	if after.StepsSinceSummary != 0 {
		t.Fatalf("expected summary steps reset, got %d", after.StepsSinceSummary)
	}
	if len(after.RecentLogs) == 0 {
		t.Fatalf("expected recent logs preserved")
	}
	if len(after.RecentObservations) == 0 {
		t.Fatalf("expected recent observations preserved")
	}
}
