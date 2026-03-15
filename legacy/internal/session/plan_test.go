package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsurePlanCreatesFile(t *testing.T) {
	temp := t.TempDir()
	sessionDir := filepath.Join(temp, "session-1")
	planPath, err := EnsurePlan(sessionDir, "plan.md")
	if err != nil {
		t.Fatalf("EnsurePlan error: %v", err)
	}
	if _, err := os.Stat(planPath); err != nil {
		t.Fatalf("plan file missing: %v", err)
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if !strings.Contains(string(data), "# Session Plan") {
		t.Fatalf("plan content missing header")
	}
}

func TestAppendPlanAddsUpdate(t *testing.T) {
	temp := t.TempDir()
	sessionDir := filepath.Join(temp, "session-2")
	planPath, err := AppendPlan(sessionDir, "plan.md", "Test plan")
	if err != nil {
		t.Fatalf("AppendPlan error: %v", err)
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Test plan") {
		t.Fatalf("expected plan update content")
	}
	if !strings.Contains(content, "## Update (UTC):") {
		t.Fatalf("expected update header")
	}
}
