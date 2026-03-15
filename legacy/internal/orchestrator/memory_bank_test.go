package orchestrator

import (
	"encoding/json"
	"fmt"
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
	for _, file := range []string{"hypotheses.md", "plan_summary.md", "known_facts.md", "open_questions.md", "context.json", memoryProvenanceFile, memoryContractFile} {
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
		"state":        FindingStateVerified,
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

func TestRefreshMemoryBankExcludesUnverifiedFindingsFromKnownFacts(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-memory-unverified-filter"
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
		"title":        "Unverified service guess",
		"state":        FindingStateCandidate,
		"severity":     "low",
		"confidence":   "medium",
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
	if strings.Contains(string(knownFactsData), "Unverified service guess") {
		t.Fatalf("did not expect unverified finding in known_facts.md:\n%s", string(knownFactsData))
	}
	if !strings.Contains(string(knownFactsData), "No verified findings yet.") {
		t.Fatalf("expected explicit no-verified-findings marker:\n%s", string(knownFactsData))
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
			"state":        FindingStateVerified,
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

func TestRefreshMemoryBankWritesFindingProvenance(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-memory-provenance"
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
	now := time.Now().UTC()
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-candidate",
		RunID:    runID,
		WorkerID: "signal-worker-t1-a1",
		TaskID:   "t1",
		Seq:      1,
		TS:       now,
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "127.0.0.1",
			"finding_type": "service",
			"title":        "SSH open",
			"state":        FindingStateCandidate,
			"severity":     "low",
			"confidence":   "medium",
		}),
	}); err != nil {
		t.Fatalf("append candidate finding: %v", err)
	}
	if err := AppendEventJSONL(manager.eventPath(runID), EventEnvelope{
		EventID:  "e-verified",
		RunID:    runID,
		WorkerID: "signal-worker-t1-a1",
		TaskID:   "t1",
		Seq:      2,
		TS:       now.Add(time.Second),
		Type:     EventTypeTaskFinding,
		Payload: mustJSONRaw(map[string]any{
			"target":       "127.0.0.1",
			"finding_type": "service",
			"title":        "SSH open",
			"state":        FindingStateVerified,
			"severity":     "low",
			"confidence":   "high",
		}),
	}); err != nil {
		t.Fatalf("append verified finding: %v", err)
	}
	if _, err := manager.IngestEvidence(runID); err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	if err := manager.RefreshMemoryBank(runID); err != nil {
		t.Fatalf("RefreshMemoryBank: %v", err)
	}
	provenancePath := filepath.Join(BuildRunPaths(base, runID).MemoryDir, memoryProvenanceFile)
	data, err := os.ReadFile(provenancePath)
	if err != nil {
		t.Fatalf("ReadFile provenance: %v", err)
	}
	var envelope memoryProvenanceEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("Unmarshal provenance: %v", err)
	}
	var sshRecord MemoryFactProvenance
	found := false
	for _, record := range envelope.Records {
		if record.Title != "SSH open" {
			continue
		}
		sshRecord = record
		found = true
		break
	}
	if !found {
		t.Fatalf("expected SSH open provenance record")
	}
	if !sshRecord.PromotedToKnownFacts {
		t.Fatalf("expected promoted known fact record")
	}
	if sshRecord.CurrentState != FindingStateVerified {
		t.Fatalf("expected current state %s, got %s", FindingStateVerified, sshRecord.CurrentState)
	}
	if len(sshRecord.SourceEventIDs) < 2 {
		t.Fatalf("expected source event IDs in provenance, got %#v", sshRecord.SourceEventIDs)
	}
	if len(sshRecord.StateTransitions) == 0 {
		t.Fatalf("expected state transition lineage in provenance")
	}
}

func TestRefreshMemoryBankWarnsOnSharedMemoryDrift(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-memory-drift-warning"
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
	knownFactsPath := filepath.Join(BuildRunPaths(base, runID).MemoryDir, "known_facts.md")
	if err := os.WriteFile(knownFactsPath, []byte("# Known Facts\n\n- tampered by worker\n"), 0o644); err != nil {
		t.Fatalf("tamper known_facts.md: %v", err)
	}
	if err := manager.RefreshMemoryBank(runID); err != nil {
		t.Fatalf("RefreshMemoryBank: %v", err)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	foundWarning := false
	for _, event := range events {
		if event.Type != EventTypeRunWarning {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if strings.TrimSpace(toString(payload["reason"])) != "shared_memory_contract_violation" {
			continue
		}
		if !strings.Contains(strings.Join(sliceFromAny(payload["files"]), ","), "known_facts.md") {
			t.Fatalf("expected known_facts.md drift detail, got %#v", payload["files"])
		}
		foundWarning = true
		break
	}
	if !foundWarning {
		t.Fatalf("expected shared memory contract violation warning event")
	}
	knownFactsData, err := os.ReadFile(knownFactsPath)
	if err != nil {
		t.Fatalf("read known_facts.md: %v", err)
	}
	if strings.Contains(string(knownFactsData), "tampered by worker") {
		t.Fatalf("expected refresh to reconcile tampered known facts")
	}
}

func TestCompactKnownFactsEntriesPreservesAnchorsAndLatestDynamic(t *testing.T) {
	values := []string{
		"Goal: test target",
		"Planner decision: goal_seed_v1",
	}
	for i := 0; i < 40; i++ {
		values = append(values, fmt.Sprintf("dynamic fact %02d", i))
	}
	retained := compactKnownFactsEntries(values, 20)
	if len(retained) != 20 {
		t.Fatalf("expected retained size 20, got %d", len(retained))
	}
	joined := strings.Join(retained, "\n")
	if !strings.Contains(joined, "Goal: test target") {
		t.Fatalf("expected Goal anchor retained")
	}
	if !strings.Contains(joined, "Planner decision: goal_seed_v1") {
		t.Fatalf("expected Planner decision anchor retained")
	}
	if !strings.Contains(joined, "dynamic fact 39") {
		t.Fatalf("expected latest dynamic fact retained")
	}
	if strings.Contains(joined, "dynamic fact 00") {
		t.Fatalf("expected oldest dynamic fact dropped")
	}
}

func TestCompactTailEntriesKeepsNewest(t *testing.T) {
	values := []string{"q1", "q2", "q3", "q4", "q5"}
	retained := compactTailEntries(values, 2)
	if len(retained) != 2 {
		t.Fatalf("expected retained size 2, got %d", len(retained))
	}
	if retained[0] != "q4" || retained[1] != "q5" {
		t.Fatalf("expected newest tail [q4 q5], got %#v", retained)
	}
}
