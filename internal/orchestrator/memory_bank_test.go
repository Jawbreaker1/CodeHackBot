package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitializeMemoryBankCreatesScaffold(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-memory-init"
	manager := NewManager(base)
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
		Metadata: PlanMetadata{
			Goal:           "inspect localhost",
			NormalizedGoal: "inspect localhost",
			PlannerVersion: "planner_v1",
			PlannerMode:    "goal_seed_v1",
			Hypotheses: []Hypothesis{
				{ID: "H-01", Statement: "Target may expose weak service", Impact: "medium", Confidence: "high", Score: 70, EvidenceRequired: []string{"service scan"}},
			},
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	memoryDir := BuildRunPaths(base, runID).MemoryDir
	for _, file := range []string{"hypotheses.md", "plan_summary.md", "known_facts.md", "open_questions.md", "context.json"} {
		if _, err := os.Stat(filepath.Join(memoryDir, file)); err != nil {
			t.Fatalf("expected memory file %s: %v", file, err)
		}
	}
}

func TestRefreshMemoryBankFoldsFindings(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-memory-refresh"
	manager := NewManager(base)
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
		Metadata: PlanMetadata{
			Goal:           "inspect localhost",
			NormalizedGoal: "inspect localhost",
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", EventTypeTaskFinding, map[string]any{
		"target":       "127.0.0.1",
		"finding_type": "service",
		"title":        "SSH open",
		"severity":     "low",
		"confidence":   "high",
	}); err != nil {
		t.Fatalf("EmitEvent finding: %v", err)
	}
	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	if err := manager.RefreshMemoryBank(runID); err != nil {
		t.Fatalf("RefreshMemoryBank: %v", err)
	}
	memoryDir := BuildRunPaths(base, runID).MemoryDir
	knownFactsData, err := os.ReadFile(filepath.Join(memoryDir, "known_facts.md"))
	if err != nil {
		t.Fatalf("read known_facts.md: %v", err)
	}
	if !strings.Contains(string(knownFactsData), "SSH open") {
		t.Fatalf("expected finding in known_facts.md:\n%s", string(knownFactsData))
	}
	ctx, err := readMemoryContext(filepath.Join(memoryDir, "context.json"))
	if err != nil {
		t.Fatalf("readMemoryContext: %v", err)
	}
	if ctx.FindingCount < 1 {
		t.Fatalf("expected finding count in context, got %d", ctx.FindingCount)
	}
	if ctx.UpdatedAt.IsZero() {
		t.Fatalf("expected updated timestamp")
	}
}

func TestRefreshMemoryBankCompactsLargeFindingSet(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-memory-compact"
	manager := NewManager(base)
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			task("t1", nil, 1),
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	now := time.Now().UTC()
	for i := 0; i < memoryMaxKnownFacts+5; i++ {
		if err := manager.EmitEvent(runID, "signal-worker-t1-a1", "t1", EventTypeTaskFinding, map[string]any{
			"target":       "127.0.0.1",
			"finding_type": "service",
			"title":        "Finding #" + strings.TrimSpace(time.Unix(now.Unix()+int64(i), 0).Format(time.RFC3339Nano)),
			"severity":     "low",
			"confidence":   "high",
		}); err != nil {
			t.Fatalf("emit finding %d: %v", i, err)
		}
	}
	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	if err := manager.RefreshMemoryBank(runID); err != nil {
		t.Fatalf("RefreshMemoryBank: %v", err)
	}
	ctx, err := readMemoryContext(filepath.Join(BuildRunPaths(base, runID).MemoryDir, "context.json"))
	if err != nil {
		t.Fatalf("readMemoryContext: %v", err)
	}
	if !ctx.Compacted {
		t.Fatalf("expected compacted memory context")
	}
}
