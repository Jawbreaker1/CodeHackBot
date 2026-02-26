package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWorkerAssistLayeredContextReadsMemoryAndArtifacts(t *testing.T) {
	base := t.TempDir()
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-layered",
		TaskID:      "T-001",
		WorkerID:    "worker-T-001-a1",
		Attempt:     2,
	}
	paths, err := EnsureRunLayout(base, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(paths.ArtifactDir, cfg.TaskID), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.MemoryDir, "known_facts.md"), []byte("# Known Facts\n\n- fact one\n- fact two\n"), 0o644); err != nil {
		t.Fatalf("write known_facts.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.MemoryDir, "open_questions.md"), []byte("# Open Questions\n\n- unknown one\n"), 0o644); err != nil {
		t.Fatalf("write open_questions.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ArtifactDir, cfg.TaskID, "scan.log"), []byte("scan output"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	ctx := buildWorkerAssistLayeredContext(cfg, []string{"command ok: list_dir -la /tmp"}, "prior failure")
	if len(ctx.KnownFacts) != 2 {
		t.Fatalf("expected 2 known facts, got %d", len(ctx.KnownFacts))
	}
	if len(ctx.OpenUnknowns) != 1 {
		t.Fatalf("expected 1 open unknown, got %d", len(ctx.OpenUnknowns))
	}
	if !strings.Contains(ctx.ChatHistory, "[recent_actions]") {
		t.Fatalf("expected recent_actions section in chat history, got: %q", ctx.ChatHistory)
	}
	if !strings.Contains(ctx.RecentLog, "[recent_artifacts]") {
		t.Fatalf("expected recent_artifacts section in recent log, got: %q", ctx.RecentLog)
	}
	if !strings.Contains(ctx.RecentLog, "[recovery_context]") {
		t.Fatalf("expected recovery_context section in recent log, got: %q", ctx.RecentLog)
	}
	if !strings.Contains(ctx.Inventory, "scan.log") {
		t.Fatalf("expected inventory to include artifact path, got: %q", ctx.Inventory)
	}
}

func TestReadMarkdownBulletsIgnoresNonBullets(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "facts.md")
	data := "# Header\nplain\n- bullet-a\n* not-counted\n- bullet-b\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	items := readMarkdownBullets(path, 10)
	if len(items) != 2 {
		t.Fatalf("expected 2 bullets, got %d (%v)", len(items), items)
	}
}

func TestBuildWorkerAssistLayeredContextIncludesMemoryCompactionSummary(t *testing.T) {
	base := t.TempDir()
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-layered-memory-summary",
		TaskID:      "T-MEM",
		WorkerID:    "worker-T-MEM-a1",
		Attempt:     1,
	}
	paths, err := EnsureRunLayout(base, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(paths.ArtifactDir, cfg.TaskID), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.MemoryDir, "known_facts.md"), []byte("# Known Facts\n\n- Goal: demo\n- Planner decision: llm\n"), 0o644); err != nil {
		t.Fatalf("write known_facts.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.MemoryDir, "open_questions.md"), []byte("# Open Questions\n\n- unknown one\n"), 0o644); err != nil {
		t.Fatalf("write open_questions.md: %v", err)
	}
	memCtx := MemoryContext{
		RunID:                 cfg.RunID,
		KnownFactsCount:       80,
		KnownFactsRetained:    20,
		KnownFactsDropped:     60,
		OpenQuestionsCount:    35,
		OpenQuestionsRetained: 20,
		OpenQuestionsDropped:  15,
		Compacted:             true,
	}
	if err := WriteJSONAtomic(filepath.Join(paths.MemoryDir, "context.json"), memCtx); err != nil {
		t.Fatalf("write context.json: %v", err)
	}

	ctx := buildWorkerAssistLayeredContext(cfg, []string{"command ok: noop"}, "")
	if !strings.Contains(ctx.RecentLog, "[compaction_summary]") {
		t.Fatalf("expected compaction summary section in recent log")
	}
	if !strings.Contains(ctx.RecentLog, "memory_bank known_facts compacted:") {
		t.Fatalf("expected memory-bank known_facts compaction summary")
	}
	if !strings.Contains(ctx.RecentLog, "memory_bank open_unknowns compacted:") {
		t.Fatalf("expected memory-bank open_unknowns compaction summary")
	}
}

func TestBuildWorkerAssistLayeredContextStressCompaction(t *testing.T) {
	base := t.TempDir()
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-layered-stress",
		TaskID:      "T-CTX",
		WorkerID:    "worker-T-CTX-a1",
		Attempt:     1,
	}
	paths, err := EnsureRunLayout(base, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	artifactDir := filepath.Join(paths.ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}

	var facts strings.Builder
	facts.WriteString("# Known Facts\n\n")
	for i := 0; i < 200; i++ {
		facts.WriteString(fmt.Sprintf("- fact-%03d %s\n", i, strings.Repeat("x", 180)))
	}
	if err := os.WriteFile(filepath.Join(paths.MemoryDir, "known_facts.md"), []byte(facts.String()), 0o644); err != nil {
		t.Fatalf("write known_facts.md: %v", err)
	}

	var unknowns strings.Builder
	unknowns.WriteString("# Open Questions\n\n")
	for i := 0; i < 120; i++ {
		unknowns.WriteString(fmt.Sprintf("- unknown-%03d %s\n", i, strings.Repeat("y", 160)))
	}
	if err := os.WriteFile(filepath.Join(paths.MemoryDir, "open_questions.md"), []byte(unknowns.String()), 0o644); err != nil {
		t.Fatalf("write open_questions.md: %v", err)
	}

	for i := 0; i < 80; i++ {
		name := fmt.Sprintf("artifact-%03d.log", i)
		content := fmt.Sprintf("artifact %d %s", i, strings.Repeat("z", 400))
		if err := os.WriteFile(filepath.Join(artifactDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write artifact %q: %v", name, err)
		}
	}

	observations := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		observations = append(observations, fmt.Sprintf("obs-%03d %s", i, strings.Repeat("o", 300)))
	}

	ctx := buildWorkerAssistLayeredContext(cfg, observations, "prior failure: command timed out")
	if len(ctx.KnownFacts) > 20 {
		t.Fatalf("known facts should be capped at 20, got %d", len(ctx.KnownFacts))
	}
	if !strings.Contains(strings.Join(ctx.KnownFacts, "\n"), "fact-199") {
		t.Fatalf("expected latest known fact retained")
	}
	if strings.Contains(strings.Join(ctx.KnownFacts, "\n"), "fact-000") {
		t.Fatalf("expected oldest known facts dropped")
	}
	if len(ctx.OpenUnknowns) > 12 {
		t.Fatalf("open unknowns should be capped at 12, got %d", len(ctx.OpenUnknowns))
	}
	if !strings.Contains(strings.Join(ctx.OpenUnknowns, "\n"), "unknown-119") {
		t.Fatalf("expected latest unknown retained")
	}
	if strings.Contains(strings.Join(ctx.OpenUnknowns, "\n"), "unknown-000") {
		t.Fatalf("expected oldest unknowns dropped")
	}
	if len(ctx.RecentActions) > 8 {
		t.Fatalf("recent actions should be capped at 8, got %d", len(ctx.RecentActions))
	}
	if !strings.Contains(strings.Join(ctx.RecentActions, "\n"), "obs-299") {
		t.Fatalf("expected latest observation retained")
	}
	if strings.Contains(strings.Join(ctx.RecentActions, "\n"), "obs-000") {
		t.Fatalf("expected oldest observations dropped")
	}
	inventoryLines := 0
	for _, line := range strings.Split(ctx.Inventory, "\n") {
		if strings.TrimSpace(line) != "" {
			inventoryLines++
		}
	}
	if inventoryLines > 10 {
		t.Fatalf("recent artifacts should be capped at 10, got %d", inventoryLines)
	}
	if len(ctx.ChatHistory) == 0 || len(ctx.RecentLog) == 0 {
		t.Fatalf("expected non-empty layered chat/recent log")
	}
	if !strings.Contains(ctx.RecentLog, "[compaction_summary]") {
		t.Fatalf("expected compaction summary section in recent log")
	}
	if !strings.Contains(ctx.RecentLog, "known_facts compacted:") {
		t.Fatalf("expected known_facts compaction note")
	}
	t.Logf("stress compaction sizes: summary=%d knownFacts=%d openUnknowns=%d recentActions=%d inventoryLines=%d chatBytes=%d recentBytes=%d",
		len(ctx.Summary), len(ctx.KnownFacts), len(ctx.OpenUnknowns), len(ctx.RecentActions), inventoryLines, len(ctx.ChatHistory), len(ctx.RecentLog))
	t.Logf("retained samples: knownFactFirst=%q knownFactLast=%q recentActionFirst=%q recentActionLast=%q",
		ctx.KnownFacts[0], ctx.KnownFacts[len(ctx.KnownFacts)-1], ctx.RecentActions[0], ctx.RecentActions[len(ctx.RecentActions)-1])
}
