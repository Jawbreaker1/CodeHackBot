package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureArtifactsCreatesFiles(t *testing.T) {
	temp := t.TempDir()
	artifacts, err := EnsureArtifacts(temp)
	if err != nil {
		t.Fatalf("EnsureArtifacts error: %v", err)
	}
	paths := []string{artifacts.SummaryPath, artifacts.FactsPath, artifacts.FocusPath, artifacts.StatePath}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing artifact %s: %v", path, err)
		}
	}
	data, err := os.ReadFile(artifacts.SummaryPath)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !strings.Contains(string(data), "# Session Summary") {
		t.Fatalf("summary header missing")
	}
}

func TestStateRoundTrip(t *testing.T) {
	temp := t.TempDir()
	statePath := filepath.Join(temp, StateFilename)
	state := State{StepsSinceSummary: 2, LastSummaryAt: "now", RecentLogs: []string{"a"}}
	if err := SaveState(statePath, state); err != nil {
		t.Fatalf("SaveState error: %v", err)
	}
	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if loaded.StepsSinceSummary != 2 || loaded.LastSummaryAt != "now" || len(loaded.RecentLogs) != 1 {
		t.Fatalf("state mismatch: %+v", loaded)
	}
}
