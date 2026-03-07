package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	task := TaskSpec{
		TaskID:            cfg.TaskID,
		Goal:              "Validate archive workflow",
		DoneWhen:          []string{"objective_met"},
		FailWhen:          []string{"objective_not_met"},
		ExpectedArtifacts: []string{"scan.log"},
	}
	ctx := buildWorkerAssistLayeredContext(cfg, task, []string{"command ok: list_dir -la /tmp"}, "prior failure", nil, nil)
	if len(ctx.KnownFacts) != 2 {
		t.Fatalf("expected 2 known facts, got %d", len(ctx.KnownFacts))
	}
	if len(ctx.OpenUnknowns) != 1 {
		t.Fatalf("expected 1 open unknown, got %d", len(ctx.OpenUnknowns))
	}
	if !strings.Contains(ctx.ChatHistory, "[recent_actions]") {
		t.Fatalf("expected recent_actions section in chat history, got: %q", ctx.ChatHistory)
	}
	if !strings.Contains(ctx.ChatHistory, "[task_contract]") {
		t.Fatalf("expected task_contract section in chat history, got: %q", ctx.ChatHistory)
	}
	if !strings.Contains(ctx.ChatHistory, "[recovery_state]") {
		t.Fatalf("expected recovery_state section in chat history, got: %q", ctx.ChatHistory)
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

	ctx := buildWorkerAssistLayeredContext(cfg, TaskSpec{TaskID: cfg.TaskID, Goal: "demo"}, []string{"command ok: noop"}, "", nil, nil)
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

	ctx := buildWorkerAssistLayeredContext(cfg, TaskSpec{TaskID: cfg.TaskID, Goal: "stress context"}, observations, "prior failure: command timed out", nil, nil)
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

func TestBuildWorkerAssistLayeredContextIncludesDependencyArtifacts(t *testing.T) {
	base := t.TempDir()
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-layered-deps",
		TaskID:      "T-002",
		WorkerID:    "worker-T-002-a1",
		Attempt:     1,
	}
	paths, err := EnsureRunLayout(base, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	currentTaskDir := filepath.Join(paths.ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(currentTaskDir, 0o755); err != nil {
		t.Fatalf("mkdir task artifact dir: %v", err)
	}
	depDir := filepath.Join(paths.ArtifactDir, "T-001")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("mkdir dep artifact dir: %v", err)
	}
	currentArtifact := filepath.Join(currentTaskDir, "current.log")
	depArtifact := filepath.Join(depDir, "dep.log")
	if err := os.WriteFile(currentArtifact, []byte("current output"), 0o644); err != nil {
		t.Fatalf("write current artifact: %v", err)
	}
	if err := os.WriteFile(depArtifact, []byte("dependency output"), 0o644); err != nil {
		t.Fatalf("write dep artifact: %v", err)
	}
	ctx := buildWorkerAssistLayeredContext(cfg, TaskSpec{TaskID: cfg.TaskID, Goal: "dependency test"}, []string{"command ok"}, "", []string{depArtifact}, nil)
	if !strings.Contains(ctx.RecentLog, "[dependency_artifacts]") {
		t.Fatalf("expected dependency_artifacts section in recent log, got: %q", ctx.RecentLog)
	}
	if !strings.Contains(ctx.Inventory, depArtifact) {
		t.Fatalf("expected dependency artifact in inventory, got: %q", ctx.Inventory)
	}
	if !strings.Contains(ctx.Inventory, currentArtifact) {
		t.Fatalf("expected current artifact in inventory, got: %q", ctx.Inventory)
	}
}

func TestBuildWorkerAssistLayeredContextPrioritizesFailureAnchorsOverInspectionChurn(t *testing.T) {
	base := t.TempDir()
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-layered-relevance",
		TaskID:      "T-FAIL",
		WorkerID:    "worker-T-FAIL-a1",
		Attempt:     2,
	}
	paths, err := EnsureRunLayout(base, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(paths.ArtifactDir, cfg.TaskID), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	observations := []string{
		"command ok: read_file /tmp/worker.log | output: still inspecting",
		"command ok: list_dir /tmp | output: file list",
		"command failed: read_file /home/johan/birdhackbot/CodeHackBot/sessions/run/orchestrator/artifact/T-03-hash-extraction/zip.hash | output: no such file",
		"attempt delta summary: observations=7->20 | anchors=4->6",
		"command ok: read_file /tmp/worker.log | output: repeated inspection",
	}
	ctx := buildWorkerAssistLayeredContext(cfg, TaskSpec{
		TaskID:            cfg.TaskID,
		Goal:              "recover zip password",
		ExpectedArtifacts: []string{"zip.hash"},
	}, observations, "prior attempt failure: no such file /home/johan/.../zip.hash", nil, nil)

	if !strings.Contains(ctx.RecentLog, "[recovery_anchors]") {
		t.Fatalf("expected recovery_anchors section in recent log, got: %q", ctx.RecentLog)
	}
	if !strings.Contains(ctx.ChatHistory, "zip.hash") {
		t.Fatalf("expected chat history to retain hash anchor, got: %q", ctx.ChatHistory)
	}
	if !strings.Contains(ctx.ChatHistory, "attempt delta summary") {
		t.Fatalf("expected attempt delta summary retained, got: %q", ctx.ChatHistory)
	}
}

func TestBuildWorkerAssistLayeredContextPrioritizesAnchoredArtifacts(t *testing.T) {
	base := t.TempDir()
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-layered-artifacts",
		TaskID:      "T-ART",
		WorkerID:    "worker-T-ART-a1",
		Attempt:     1,
	}
	paths, err := EnsureRunLayout(base, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	taskDir := filepath.Join(paths.ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	anchored := filepath.Join(taskDir, "zip.hash")
	if err := os.WriteFile(anchored, []byte("hash"), 0o644); err != nil {
		t.Fatalf("write anchored artifact: %v", err)
	}
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(anchored, old, old); err != nil {
		t.Fatalf("chtimes anchored artifact: %v", err)
	}
	for i := 0; i < 12; i++ {
		path := filepath.Join(taskDir, fmt.Sprintf("worker-T-ART-a1-s%d.log", i))
		if err := os.WriteFile(path, []byte("inspection"), 0o644); err != nil {
			t.Fatalf("write worker log: %v", err)
		}
	}

	ctx := buildWorkerAssistLayeredContext(cfg, TaskSpec{
		TaskID:            cfg.TaskID,
		Goal:              "recover zip password",
		ExpectedArtifacts: []string{"zip.hash"},
	}, []string{"command failed: read_file /tmp/zip.hash | output: no such file"}, "recover using zip.hash", nil, nil)

	if !strings.Contains(ctx.RecentLog, anchored) {
		t.Fatalf("expected anchored artifact retained in recent log, got: %q", ctx.RecentLog)
	}
	if !strings.Contains(ctx.Inventory, anchored) {
		t.Fatalf("expected anchored artifact retained in inventory, got: %q", ctx.Inventory)
	}
}

func TestBuildWorkerAssistLayeredContextIncludesExplicitRecoveryState(t *testing.T) {
	base := t.TempDir()
	cfg := WorkerRunConfig{
		SessionsDir: base,
		RunID:       "run-layered-recovery-state",
		TaskID:      "T-REC",
		WorkerID:    "worker-T-REC-a1",
		Attempt:     1,
	}
	paths, err := EnsureRunLayout(base, cfg.RunID)
	if err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	taskDir := filepath.Join(paths.ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	logPath := filepath.Join(taskDir, "worker-T-REC-a1-a1-s1-t1.log")
	if err := os.WriteFile(logPath, []byte("zipinfo output"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	feedback := &assistExecutionFeedback{
		Command:             "read_file",
		Args:                []string{"/tmp/archive_metadata.txt"},
		ExitCode:            0,
		ResultSummary:       "command ok: read_file /tmp/archive_metadata.txt | output: Archive contains secret_text.txt",
		PrimaryArtifactRefs: []string{"/tmp/archive_metadata.txt"},
		LogPath:             logPath,
		CombinedOutputTail:  "Archive contains secret_text.txt",
	}

	ctx := buildWorkerAssistLayeredContext(cfg, TaskSpec{
		TaskID:            cfg.TaskID,
		Goal:              "inspect metadata",
		ExpectedArtifacts: []string{"archive_metadata.txt", "password.txt"},
	}, []string{"command ok: read_file /tmp/archive_metadata.txt | output: Archive contains secret_text.txt"}, "", nil, feedback)

	if !strings.Contains(ctx.RecentLog, "previous_command: read_file /tmp/archive_metadata.txt") {
		t.Fatalf("expected previous_command in recovery state, got: %q", ctx.RecentLog)
	}
	if !strings.Contains(ctx.RecentLog, "previous_result_summary: command ok: read_file /tmp/archive_metadata.txt") {
		t.Fatalf("expected previous_result_summary in recovery state, got: %q", ctx.RecentLog)
	}
	if !strings.Contains(ctx.RecentLog, "primary_evidence_refs: /tmp/archive_metadata.txt") {
		t.Fatalf("expected primary_evidence_refs in recovery state, got: %q", ctx.RecentLog)
	}
	if !strings.Contains(ctx.RecentLog, "contract_gap: missing_expected_artifacts=archive_metadata.txt, password.txt") {
		t.Fatalf("expected contract gap in recovery state, got: %q", ctx.RecentLog)
	}
}
