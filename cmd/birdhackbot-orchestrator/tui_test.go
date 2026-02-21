package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestParseTUICommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		name string
	}{
		{in: "", name: "refresh"},
		{in: "help", name: "help"},
		{in: "plan", name: "plan"},
		{in: "tasks", name: "tasks"},
		{in: "ask what is current status", name: "ask"},
		{in: "instruct investigate target", name: "instruct"},
		{in: "execute", name: "execute"},
		{in: "regenerate", name: "regenerate"},
		{in: "discard", name: "discard"},
		{in: "task add verify tls configuration", name: "task_add"},
		{in: "task remove task-manual-001", name: "task_remove"},
		{in: "task set task-manual-001 risk active_probe", name: "task_set"},
		{in: "task move task-manual-001 2", name: "task_move"},
		{in: "Hello orchestrator", name: "ask"},
		{in: "how many workers are running?", name: "ask"},
		{in: "scan the current network again", name: "ask"},
		{in: "q", name: "quit"},
		{in: "events 25", name: "events"},
		{in: "events up 3", name: "events"},
		{in: "events down", name: "events"},
		{in: "log up 3", name: "log"},
		{in: "log down", name: "log"},
		{in: "approve apr-1 task ok", name: "approve"},
		{in: "deny apr-2 nope", name: "deny"},
		{in: "stop", name: "stop"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			cmd, err := parseTUICommand(tc.in)
			if err != nil {
				t.Fatalf("parseTUICommand returned error: %v", err)
			}
			if cmd.name != tc.name {
				t.Fatalf("unexpected name: got %s want %s", cmd.name, tc.name)
			}
		})
	}
}

func TestParseTUICommandRejectsInvalid(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"events nope",
		"events up nope",
		"log sideways",
		"approve",
		"deny",
		"ask",
		"instruct",
		"task",
		"task add",
		"task remove",
		"task set task-1 risk",
		"task move task-1 nope",
	}
	for _, in := range invalid {
		in := in
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			if _, err := parseTUICommand(in); err == nil {
				t.Fatalf("expected parse error for %q", in)
			}
		})
	}
}

func TestExecuteTUICommandTaskEditsMutatePlan(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-plan-edits"
	manager := orchestrator.NewManager(base)
	plan := buildInteractivePlanningPlan(
		runID,
		time.Now().UTC(),
		orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		2,
	)
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}

	scroll := 0
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "task_add", reason: "baseline recon"}, &scroll); done || !strings.Contains(log, "task added") {
		t.Fatalf("expected task add success, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "task_add", reason: "service validation"}, &scroll); done || !strings.Contains(log, "task added") {
		t.Fatalf("expected second task add success, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "task_set", taskID: "task-manual-001", field: "risk", value: "active_probe"}, &scroll); done || !strings.Contains(log, "task updated") {
		t.Fatalf("expected task risk update, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "task_set", taskID: "task-manual-001", field: "targets", value: "127.0.0.1,localhost"}, &scroll); done || !strings.Contains(log, "task updated") {
		t.Fatalf("expected task target update, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "task_move", taskID: "task-manual-002", position: 1}, &scroll); done || !strings.Contains(log, "task moved") {
		t.Fatalf("expected task move success, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "task_remove", taskID: "task-manual-001"}, &scroll); done || !strings.Contains(log, "task removed") {
		t.Fatalf("expected task remove success, got done=%v log=%q", done, log)
	}

	updated, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if len(updated.Tasks) != 1 {
		t.Fatalf("expected 1 task after removal, got %d", len(updated.Tasks))
	}
	remaining := updated.Tasks[0]
	if remaining.TaskID != "task-manual-002" {
		t.Fatalf("expected remaining task-manual-002, got %s", remaining.TaskID)
	}
	if phase := orchestrator.NormalizeRunPhase(updated.Metadata.RunPhase); phase != orchestrator.RunPhaseReview {
		t.Fatalf("expected review phase after task edits, got %q", updated.Metadata.RunPhase)
	}
	if remaining.RiskLevel == "" {
		t.Fatalf("expected task risk level to be set")
	}
}

func TestPlanningRefinementDeterministicAcrossRuns(t *testing.T) {
	base := t.TempDir()
	instructions := []string{
		"map localhost services and summarize findings",
		"also include tls checks and prioritize externally reachable services",
	}
	firstSig := runPlanningInstructionSequence(t, base, "run-plan-deterministic-a", instructions)
	secondSig := runPlanningInstructionSequence(t, base, "run-plan-deterministic-b", instructions)
	if firstSig != secondSig {
		t.Fatalf("expected deterministic plan signature, got %q vs %q", firstSig, secondSig)
	}
}

func TestPlanningArtifactsPersistRevisionsAndProvenance(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-plan-artifacts"
	manager := orchestrator.NewManager(base)
	plan := buildInteractivePlanningPlan(
		runID,
		time.Now().UTC(),
		orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		2,
	)
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}

	scroll := 0
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "instruct", reason: "scan localhost services and summarize findings"}, &scroll); done || !strings.Contains(log, "plan draft updated") {
		t.Fatalf("expected instruct update, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "task_add", reason: "manual validation pass"}, &scroll); done || !strings.Contains(log, "task added") {
		t.Fatalf("expected task add update, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "ask", reason: "can you summarize the current plan?"}, &scroll); done || !strings.Contains(log, "plan") {
		t.Fatalf("expected ask response, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "execute"}, &scroll); !done || !strings.Contains(log, "execution approved") {
		t.Fatalf("expected execute approval, got done=%v log=%q", done, log)
	}

	paths := orchestrator.BuildRunPaths(base, runID)
	indexPath := filepath.Join(paths.PlanDir, "revisions", "index.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("ReadFile revision index: %v", err)
	}
	var index []planRevisionRecord
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatalf("Unmarshal revision index: %v", err)
	}
	if len(index) < 2 {
		t.Fatalf("expected >=2 revision records, got %d", len(index))
	}
	for _, record := range index {
		if strings.TrimSpace(record.PlanPath) == "" || strings.TrimSpace(record.DiffPath) == "" {
			t.Fatalf("expected revision record with plan/diff paths: %+v", record)
		}
		if _, err := os.Stat(filepath.Join(paths.PlanDir, "revisions", record.PlanPath)); err != nil {
			t.Fatalf("missing revision plan file %s: %v", record.PlanPath, err)
		}
		if _, err := os.Stat(filepath.Join(paths.PlanDir, "revisions", record.DiffPath)); err != nil {
			t.Fatalf("missing revision diff file %s: %v", record.DiffPath, err)
		}
	}

	transcriptPath := filepath.Join(paths.PlanDir, "planner_transcript.jsonl")
	transcriptData, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("ReadFile transcript: %v", err)
	}
	text := string(transcriptData)
	if !strings.Contains(text, "\"action\":\"instruction_draft\"") {
		t.Fatalf("expected instruction draft transcript entry")
	}
	if !strings.Contains(text, "\"action\":\"task_add\"") {
		t.Fatalf("expected task_add transcript entry")
	}
	if !strings.Contains(text, "\"action\":\"execute\"") {
		t.Fatalf("expected execute transcript entry")
	}

	provenancePath := filepath.Join(paths.PlanDir, "final_approved_provenance.json")
	provenanceData, err := os.ReadFile(provenancePath)
	if err != nil {
		t.Fatalf("ReadFile provenance: %v", err)
	}
	provenance := map[string]any{}
	if err := json.Unmarshal(provenanceData, &provenance); err != nil {
		t.Fatalf("Unmarshal provenance: %v", err)
	}
	if strings.TrimSpace(stringFromAny(provenance["planner_prompt_hash"])) == "" {
		t.Fatalf("expected planner prompt hash in provenance")
	}
	if _, ok := provenance["operator_approvals"]; !ok {
		t.Fatalf("expected operator_approvals field in provenance")
	}
}

func runPlanningInstructionSequence(t *testing.T, base, runID string, instructions []string) string {
	t.Helper()
	manager := orchestrator.NewManager(base)
	plan := buildInteractivePlanningPlan(
		runID,
		time.Now().UTC(),
		orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		2,
	)
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scroll := 0
	for _, instruction := range instructions {
		done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "instruct", reason: instruction}, &scroll)
		if done || !strings.Contains(log, "plan draft updated") {
			t.Fatalf("expected deterministic plan draft update for %q, got done=%v log=%q", instruction, done, log)
		}
	}
	updated, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	normalized := make([]orchestrator.TaskSpec, len(updated.Tasks))
	copy(normalized, updated.Tasks)
	for i := range normalized {
		normalized[i].IdempotencyKey = ""
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("Marshal normalized tasks: %v", err)
	}
	return string(data)
}

func TestLooksLikeQuestion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want bool
	}{
		{in: "how many workers are running?", want: true},
		{in: "What is the current plan", want: true},
		{in: "can you summarize", want: true},
		{in: "scan the network now", want: false},
		{in: "resume execution", want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeQuestion(tc.in); got != tc.want {
				t.Fatalf("looksLikeQuestion(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestExecuteTUICommandApproveDenyStop(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-exec"
	manager := orchestrator.NewManager(base)
	if _, err := orchestrator.EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunStarted, map[string]any{
		"source": "test",
	}); err != nil {
		t.Fatalf("EmitEvent run_started: %v", err)
	}

	scroll := 0
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "approve", approval: "apr-1", scope: "task", reason: "ok"}, &scroll); done || log == "" {
		t.Fatalf("expected approve command to continue with log, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "deny", approval: "apr-2", reason: "no"}, &scroll); done || log == "" {
		t.Fatalf("expected deny command to continue with log, got done=%v log=%q", done, log)
	}
	if done, _ := executeTUICommand(manager, runID, nil, tuiCommand{name: "stop"}, &scroll); done {
		t.Fatalf("expected stop to keep tui open")
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "instruct", reason: "enumerate open ports"}, &scroll); done || !strings.Contains(log, "instruction queued") {
		t.Fatalf("expected instruct command to queue instruction, got done=%v log=%q", done, log)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEvent(events, orchestrator.EventTypeApprovalGranted) {
		t.Fatalf("expected approval_granted event")
	}
	if !hasEvent(events, orchestrator.EventTypeApprovalDenied) {
		t.Fatalf("expected approval_denied event")
	}
	if !hasEvent(events, orchestrator.EventTypeRunStopped) {
		t.Fatalf("expected run_stopped event")
	}
	if !hasEvent(events, orchestrator.EventTypeOperatorInstruction) {
		t.Fatalf("expected operator_instruction event")
	}
}

func TestAskReadOnlyAndInstructQueuesExecutionChange(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-ask-instruct-contract"
	manager := orchestrator.NewManager(base)
	if _, err := orchestrator.EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunStarted, map[string]any{
		"source": "test",
	}); err != nil {
		t.Fatalf("EmitEvent run_started: %v", err)
	}

	scroll := 0
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "ask", reason: "scan localhost services"}, &scroll); done || strings.TrimSpace(log) == "" || strings.Contains(strings.ToLower(log), "instruction queued") {
		t.Fatalf("expected ask response without exit, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "instruct", reason: "scan localhost services"}, &scroll); done || !strings.Contains(log, "instruction queued") {
		t.Fatalf("expected instruct queue log, got done=%v log=%q", done, log)
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	count := 0
	for _, event := range events {
		if event.Type == orchestrator.EventTypeOperatorInstruction {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one queued operator instruction from instruct, got %d", count)
	}
}

func TestExecuteTUICommandInstructUpdatesPlanningDraft(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-plan-draft"
	manager := orchestrator.NewManager(base)
	plan := buildInteractivePlanningPlan(
		runID,
		time.Now().UTC(),
		orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		2,
	)
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}

	scroll := 0
	done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "instruct", reason: "scan localhost services and summarize findings"}, &scroll)
	if done {
		t.Fatalf("expected tui to remain open")
	}
	if !strings.Contains(log, "plan draft updated:") {
		t.Fatalf("unexpected command log: %q", log)
	}

	updated, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if len(updated.Tasks) == 0 {
		t.Fatalf("expected synthesized tasks after planning instruction")
	}
	if phase := orchestrator.NormalizeRunPhase(updated.Metadata.RunPhase); phase != orchestrator.RunPhaseReview {
		t.Fatalf("expected review phase, got %q", updated.Metadata.RunPhase)
	}
	if strings.TrimSpace(updated.Metadata.Goal) == "" {
		t.Fatalf("expected goal metadata after planning instruction")
	}
	diffPath := filepath.Join(orchestrator.BuildRunPaths(base, runID).PlanDir, "plan.diff.txt")
	diffData, err := os.ReadFile(diffPath)
	if err != nil {
		t.Fatalf("ReadFile plan diff: %v", err)
	}
	if !strings.Contains(string(diffData), "diff +") {
		t.Fatalf("expected plan diff summary, got %q", string(diffData))
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if hasEvent(events, orchestrator.EventTypeOperatorInstruction) {
		t.Fatalf("did not expect operator instruction event in planning-mode instruct")
	}
}

func TestExecuteTUICommandExecuteApprovesDraft(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-plan-execute"
	manager := orchestrator.NewManager(base)
	plan := buildInteractivePlanningPlan(
		runID,
		time.Now().UTC(),
		orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		2,
	)
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}

	scroll := 0
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "execute"}, &scroll); done || !strings.Contains(log, "no plan tasks") {
		t.Fatalf("expected execute to fail without draft plan, got done=%v log=%q", done, log)
	}
	done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "instruct", reason: "scan localhost services and summarize findings"}, &scroll)
	if done || !strings.Contains(log, "plan draft updated") {
		t.Fatalf("expected instruct plan update, got done=%v log=%q", done, log)
	}
	done, log = executeTUICommand(manager, runID, nil, tuiCommand{name: "execute"}, &scroll)
	if !done {
		t.Fatalf("expected execute to exit planning tui, log=%q", log)
	}
	if !strings.Contains(log, "execution approved") {
		t.Fatalf("unexpected execute log: %q", log)
	}

	updated, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if phase := orchestrator.NormalizeRunPhase(updated.Metadata.RunPhase); phase != orchestrator.RunPhaseApproved {
		t.Fatalf("expected approved phase, got %q", updated.Metadata.RunPhase)
	}
}

func TestExecuteTUICommandExecuteBlocksInvalidDraft(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-plan-invalid"
	manager := orchestrator.NewManager(base)
	plan := buildInteractivePlanningPlan(
		runID,
		time.Now().UTC(),
		orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		2,
	)
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	scroll := 0
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "instruct", reason: "scan localhost services and summarize findings"}, &scroll); done || !strings.Contains(log, "plan draft updated") {
		t.Fatalf("expected instruct plan update, got done=%v log=%q", done, log)
	}

	updated, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if len(updated.Tasks) == 0 {
		t.Fatalf("expected synthesized tasks")
	}
	updated.Tasks[0].Targets = []string{"8.8.8.8"}
	updated.Metadata.RunPhase = orchestrator.RunPhaseReview
	planPath := filepath.Join(orchestrator.BuildRunPaths(base, runID).PlanDir, "plan.json")
	if err := orchestrator.WriteJSONAtomic(planPath, updated); err != nil {
		t.Fatalf("WriteJSONAtomic: %v", err)
	}

	done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "execute"}, &scroll)
	if done {
		t.Fatalf("expected execute to remain in TUI on validation failure")
	}
	if !strings.Contains(log, "plan validation failed") {
		t.Fatalf("expected validation failure, got %q", log)
	}
}

func TestExecuteTUICommandRegenerateAndDiscard(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-plan-regen"
	manager := orchestrator.NewManager(base)
	plan := buildInteractivePlanningPlan(
		runID,
		time.Now().UTC(),
		orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		2,
	)
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}

	scroll := 0
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "instruct", reason: "scan localhost services and summarize findings"}, &scroll); done || !strings.Contains(log, "plan draft updated") {
		t.Fatalf("expected instruct update, got done=%v log=%q", done, log)
	}
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "regenerate"}, &scroll); done || !strings.Contains(log, "plan regenerated") {
		t.Fatalf("expected regenerate update, got done=%v log=%q", done, log)
	}
	updated, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if len(updated.Tasks) == 0 {
		t.Fatalf("expected regenerated tasks")
	}
	if phase := orchestrator.NormalizeRunPhase(updated.Metadata.RunPhase); phase != orchestrator.RunPhaseReview {
		t.Fatalf("expected review phase after regenerate, got %q", updated.Metadata.RunPhase)
	}

	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "discard"}, &scroll); done || !strings.Contains(log, "plan discarded") {
		t.Fatalf("expected discard reset, got done=%v log=%q", done, log)
	}
	reset, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan reset: %v", err)
	}
	if len(reset.Tasks) != 0 {
		t.Fatalf("expected discarded plan to clear tasks, got %d", len(reset.Tasks))
	}
	if phase := orchestrator.NormalizeRunPhase(reset.Metadata.RunPhase); phase != orchestrator.RunPhasePlanning {
		t.Fatalf("expected planning phase after discard, got %q", reset.Metadata.RunPhase)
	}
}

func TestWaitForApprovalID(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-approval-wait"
	manager := orchestrator.NewManager(base)
	if _, err := orchestrator.EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}

	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = manager.EmitEvent(runID, "orchestrator", "t1", orchestrator.EventTypeApprovalRequested, map[string]any{
			"approval_id": "apr-wait-1",
			"tier":        "active_probe",
			"reason":      "test",
			"expires_at":  time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		})
	}()

	id := waitForApprovalID(t, manager, runID, 2*time.Second)
	if id != "apr-wait-1" {
		t.Fatalf("unexpected approval id: %s", id)
	}
}

func TestLatestFailureFromEvents_TaskFailed(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []orchestrator.EventEnvelope{
		{
			EventID:  "e1",
			RunID:    "run-1",
			WorkerID: "worker-a",
			TaskID:   "task-a",
			Seq:      1,
			TS:       now,
			Type:     orchestrator.EventTypeTaskFailed,
			Payload:  mustPayload(t, map[string]any{"reason": "assist_budget_exhausted", "error": "exhausted max steps", "log_path": "/tmp/worker.log"}),
		},
	}

	failure := latestFailureFromEvents(events)
	if failure == nil {
		t.Fatalf("expected failure")
	}
	if failure.Reason != "assist_budget_exhausted" {
		t.Fatalf("unexpected reason: %s", failure.Reason)
	}
	if failure.Error != "exhausted max steps" {
		t.Fatalf("unexpected error: %s", failure.Error)
	}
	if failure.LogPath != "/tmp/worker.log" {
		t.Fatalf("unexpected log path: %s", failure.LogPath)
	}
}

func TestLatestFailureFromEvents_WorkerStoppedFallback(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []orchestrator.EventEnvelope{
		{
			EventID:  "e1",
			RunID:    "run-1",
			WorkerID: "worker-a",
			Seq:      2,
			TS:       now,
			Type:     orchestrator.EventTypeWorkerStopped,
			Payload:  mustPayload(t, map[string]any{"status": "failed", "error": "exit status 1", "log_path": "/tmp/worker.log"}),
		},
	}

	failure := latestFailureFromEvents(events)
	if failure == nil {
		t.Fatalf("expected failure")
	}
	if failure.Reason != "worker_stopped_failed" {
		t.Fatalf("unexpected reason: %s", failure.Reason)
	}
	if failure.Error != "exit status 1" {
		t.Fatalf("unexpected error: %s", failure.Error)
	}
}

func TestLatestTaskProgressByTask(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []orchestrator.EventEnvelope{
		{
			EventID:  "e1",
			RunID:    "run-1",
			WorkerID: "worker-a",
			TaskID:   "task-1",
			Seq:      1,
			TS:       now,
			Type:     orchestrator.EventTypeTaskStarted,
			Payload:  mustPayload(t, map[string]any{"goal": "seed recon"}),
		},
		{
			EventID:  "e2",
			RunID:    "run-1",
			WorkerID: "worker-a",
			TaskID:   "task-1",
			Seq:      2,
			TS:       now.Add(2 * time.Second),
			Type:     orchestrator.EventTypeTaskProgress,
			Payload:  mustPayload(t, map[string]any{"step": 2, "tool_calls": 3, "message": "running nmap"}),
		},
		{
			EventID:  "e3",
			RunID:    "run-1",
			WorkerID: "worker-b",
			TaskID:   "task-2",
			Seq:      1,
			TS:       now.Add(time.Second),
			Type:     orchestrator.EventTypeTaskProgress,
			Payload:  mustPayload(t, map[string]any{"steps": 1, "tool_call": 1, "type": "plan"}),
		},
	}

	progress := latestTaskProgressByTask(events)
	task1, ok := progress["task-1"]
	if !ok {
		t.Fatalf("expected task-1 progress")
	}
	if task1.Step != 2 || task1.ToolCalls != 3 || task1.Message != "running nmap" {
		t.Fatalf("unexpected task-1 progress: %#v", task1)
	}

	task2, ok := progress["task-2"]
	if !ok {
		t.Fatalf("expected task-2 progress")
	}
	if task2.Step != 1 || task2.ToolCalls != 1 || task2.Message != "plan" {
		t.Fatalf("unexpected task-2 progress: %#v", task2)
	}
}

func TestLatestWorkerDebugByWorker(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []orchestrator.EventEnvelope{
		{
			EventID:  "e1",
			RunID:    "run-1",
			WorkerID: "signal-worker-a",
			TaskID:   "task-1",
			Seq:      1,
			TS:       now,
			Type:     orchestrator.EventTypeTaskProgress,
			Payload:  mustPayload(t, map[string]any{"message": "running nmap", "step": 2, "tool_calls": 1}),
		},
		{
			EventID:  "e2",
			RunID:    "run-1",
			WorkerID: "signal-worker-a",
			TaskID:   "task-1",
			Seq:      2,
			TS:       now.Add(time.Second),
			Type:     orchestrator.EventTypeTaskArtifact,
			Payload:  mustPayload(t, map[string]any{"command": "nmap", "args": []string{"-sV", "192.168.50.10"}, "path": "sessions/run-1/logs/nmap.log"}),
		},
	}

	debug := latestWorkerDebugByWorker(events)
	snapshot, ok := debug["worker-a"]
	if !ok {
		t.Fatalf("expected normalized worker key")
	}
	if snapshot.Command != "nmap" {
		t.Fatalf("unexpected command: %s", snapshot.Command)
	}
	if len(snapshot.Args) != 2 || snapshot.Args[0] != "-sV" {
		t.Fatalf("unexpected args: %#v", snapshot.Args)
	}
	if snapshot.LogPath != "sessions/run-1/logs/nmap.log" {
		t.Fatalf("unexpected log path: %s", snapshot.LogPath)
	}
}

func TestFormatProgressSummary(t *testing.T) {
	t.Parallel()

	got := formatProgressSummary(tuiTaskProgress{
		Step:      3,
		ToolCalls: 5,
		Message:   "enumerating services",
	})
	want := "step 3 | enumerating services | tools:5"
	if got != want {
		t.Fatalf("unexpected summary: got %q want %q", got, want)
	}
}

func TestClampTUIBodyLinesKeepsTopBar(t *testing.T) {
	t.Parallel()

	lines := []string{"bar", "updated", "", "plan", "workers", "events", "log"}
	clamped := clampTUIBodyLines(lines, 5)
	if len(clamped) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(clamped))
	}
	if clamped[0] != "bar" || clamped[1] != "updated" || clamped[2] != "" {
		t.Fatalf("expected top header to be preserved, got %#v", clamped[:3])
	}
}

func TestAppendLogLinesSplitsAndCaps(t *testing.T) {
	t.Parallel()

	lines := []string{"old-1", "old-2"}
	updated := appendLogLines(lines, "line-a\nline-b\n\nline-c")
	if got, want := len(updated), 5; got != want {
		t.Fatalf("expected %d lines, got %d (%#v)", want, got, updated)
	}
	if updated[2] != "line-a" || updated[4] != "line-c" {
		t.Fatalf("unexpected appended lines: %#v", updated)
	}

	over := append([]string{}, updated...)
	for i := 0; i < 320; i++ {
		over = appendLogLines(over, "entry")
	}
	if len(over) != tuiCommandLogStoreLines {
		t.Fatalf("expected capped log length %d, got %d", tuiCommandLogStoreLines, len(over))
	}
}

func TestRenderCommandLogPreservesLongAssistantReplyWithScroll(t *testing.T) {
	t.Parallel()

	messages := []string{}
	lines := []string{"assistant: CRITICAL-START"}
	for i := 0; i < 24; i++ {
		lines = append(lines, "assistant: detail line "+strconv.Itoa(i+1))
	}
	lines = append(lines, "assistant: CRITICAL-END")
	messages = appendLogLines(messages, strings.Join(lines, "\n"))

	scroll := 0
	bottom := strings.Join(renderCommandLog(messages, 80, &scroll), "\n")
	if !strings.Contains(bottom, "CRITICAL-END") {
		t.Fatalf("expected bottom view to include end marker, got %q", bottom)
	}

	scroll = 1 << 20
	top := strings.Join(renderCommandLog(messages, 80, &scroll), "\n")
	if !strings.Contains(top, "CRITICAL-START") {
		t.Fatalf("expected top view to include start marker, got %q", top)
	}
}

func TestExecuteTUICommandLogScroll(t *testing.T) {
	t.Parallel()

	manager := orchestrator.NewManager(t.TempDir())
	scroll := 0

	if done, log := executeTUICommand(manager, "run-any", nil, tuiCommand{name: "log", scope: "up", logCount: 3}, &scroll); done || !strings.Contains(log, "up 3") {
		t.Fatalf("unexpected log up result: done=%v log=%q", done, log)
	}
	if scroll != 3 {
		t.Fatalf("expected scroll=3, got %d", scroll)
	}
	if done, _ := executeTUICommand(manager, "run-any", nil, tuiCommand{name: "log", scope: "down", logCount: 2}, &scroll); done {
		t.Fatalf("expected down to continue")
	}
	if scroll != 1 {
		t.Fatalf("expected scroll=1 after down, got %d", scroll)
	}
	if done, _ := executeTUICommand(manager, "run-any", nil, tuiCommand{name: "log", scope: "bottom"}, &scroll); done {
		t.Fatalf("expected bottom to continue")
	}
	if scroll != 0 {
		t.Fatalf("expected scroll reset to 0, got %d", scroll)
	}
}

func TestExecuteTUICommandEventScroll(t *testing.T) {
	t.Parallel()

	manager := orchestrator.NewManager(t.TempDir())
	eventScroll := 0

	if done, log := executeTUICommandWithScroll(manager, "run-any", nil, tuiCommand{name: "events", scope: "up", eventLimit: 3}, nil, &eventScroll); done || !strings.Contains(log, "up 3") {
		t.Fatalf("unexpected events up result: done=%v log=%q", done, log)
	}
	if eventScroll != 3 {
		t.Fatalf("expected event scroll=3, got %d", eventScroll)
	}
	if done, _ := executeTUICommandWithScroll(manager, "run-any", nil, tuiCommand{name: "events", scope: "down", eventLimit: 2}, nil, &eventScroll); done {
		t.Fatalf("expected events down to continue")
	}
	if eventScroll != 1 {
		t.Fatalf("expected event scroll=1 after down, got %d", eventScroll)
	}
	if done, _ := executeTUICommandWithScroll(manager, "run-any", nil, tuiCommand{name: "events", scope: "bottom"}, nil, &eventScroll); done {
		t.Fatalf("expected events bottom to continue")
	}
	if eventScroll != 0 {
		t.Fatalf("expected event scroll reset to 0, got %d", eventScroll)
	}
}

func TestRenderTwoPaneWide(t *testing.T) {
	t.Parallel()

	layout := computePaneLayout(140)
	if !layout.enabled {
		t.Fatalf("expected two-pane layout to be enabled")
	}
	left := []string{"Plan:", "  - Goal: demo"}
	right := []string{"Worker Debug:", "+-box-+"}
	lines := renderTwoPane(left, right, 140, layout)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "Plan:") || !strings.Contains(lines[0], "Worker Debug:") {
		t.Fatalf("expected left/right content on same row, got %q", lines[0])
	}
}

func TestRenderRecentEventsSupportsScroll(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC)
	events := make([]orchestrator.EventEnvelope, 0, 14)
	for i := 0; i < 14; i++ {
		events = append(events, orchestrator.EventEnvelope{
			EventID:  "e-" + strconv.Itoa(i),
			RunID:    "run-1",
			WorkerID: "worker-1",
			TaskID:   "task-1",
			Seq:      int64(i + 1),
			TS:       now.Add(time.Duration(i) * time.Second),
			Type:     orchestrator.EventTypeTaskProgress,
		})
	}
	scroll := 0
	bottom := strings.Join(renderRecentEvents(events, &scroll), "\n")
	if !strings.Contains(bottom, "12:00:13") {
		t.Fatalf("expected newest event in bottom view, got %q", bottom)
	}

	scroll = 1 << 20
	top := strings.Join(renderRecentEvents(events, &scroll), "\n")
	if !strings.Contains(top, "12:00:00") {
		t.Fatalf("expected oldest event in top view, got %q", top)
	}
}

func TestRenderTwoPaneNarrowFallsBackToStack(t *testing.T) {
	t.Parallel()

	layout := computePaneLayout(80)
	if layout.enabled {
		t.Fatalf("expected two-pane layout to be disabled for narrow widths")
	}
	left := []string{"Plan:"}
	right := []string{"Worker Debug:"}
	lines := renderTwoPane(left, right, 80, layout)
	if len(lines) < 3 {
		t.Fatalf("expected stacked output with spacer, got %d lines", len(lines))
	}
	if !strings.Contains(lines[0], "Plan:") {
		t.Fatalf("expected left content first, got %q", lines[0])
	}
	if !strings.Contains(lines[2], "Worker Debug:") {
		t.Fatalf("expected right content after spacer, got %q", lines[2])
	}
}

func TestRenderTwoPaneCapsRightPaneHeight(t *testing.T) {
	t.Parallel()

	layout := computePaneLayout(140)
	if !layout.enabled {
		t.Fatalf("expected two-pane layout to be enabled")
	}
	left := []string{"Plan:", "Execution:", "Task Board:"}
	right := []string{"Worker Debug:", "box1", "box2", "box3", "box4"}
	lines := renderTwoPane(left, right, 140, layout)
	if len(lines) != len(left) {
		t.Fatalf("expected lines to match left pane height (%d), got %d", len(left), len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "...") {
		t.Fatalf("expected truncated marker in last row, got %q", lines[len(lines)-1])
	}
}

func TestRenderWorkerBoxesOrdersActiveBeforeStopped(t *testing.T) {
	t.Parallel()

	workers := []orchestrator.WorkerStatus{
		{
			WorkerID:  "worker-stopped",
			State:     "stopped",
			LastEvent: time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		},
		{
			WorkerID:  "worker-active",
			State:     "active",
			LastEvent: time.Date(2026, 2, 20, 10, 1, 0, 0, time.UTC),
		},
	}

	lines := renderWorkerBoxes(workers, map[string]tuiWorkerDebug{
		"worker-stopped": {Reason: "completed"},
	}, 60)
	idxActive := -1
	idxCollapsed := -1
	for i, line := range lines {
		if strings.Contains(line, "Worker worker-active") {
			idxActive = i
		}
		if strings.Contains(line, "Collapsed Workers") {
			idxCollapsed = i
		}
	}
	if idxActive < 0 || idxCollapsed < 0 {
		t.Fatalf("expected active and collapsed worker sections in output")
	}
	if idxActive >= idxCollapsed {
		t.Fatalf("expected active worker section before collapsed section (active=%d collapsed=%d)", idxActive, idxCollapsed)
	}
	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "worker-stopped state=stopped") {
		t.Fatalf("expected stopped worker summary in collapsed section, got %q", output)
	}
}

func TestRenderWorkerBoxesSnapshotCollapsedByDefault(t *testing.T) {
	t.Parallel()

	workers := []orchestrator.WorkerStatus{
		{WorkerID: "worker-active-1", State: "active", CurrentTask: "task-a", LastSeq: 12},
		{WorkerID: "worker-stopped-1", State: "stopped", CurrentTask: "task-b"},
		{WorkerID: "worker-stopped-2", State: "stopped", CurrentTask: "task-c"},
	}
	debug := map[string]tuiWorkerDebug{
		"worker-active-1":  {Message: "running nmap"},
		"worker-stopped-1": {Reason: "completed"},
		"worker-stopped-2": {Reason: "failed"},
	}
	lines := renderWorkerBoxes(workers, debug, 72)
	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "Worker worker-active-1") {
		t.Fatalf("snapshot missing expanded active worker block: %q", output)
	}
	if !strings.Contains(output, "Collapsed Workers") {
		t.Fatalf("snapshot missing collapsed workers block: %q", output)
	}
	if !strings.Contains(output, "2 stopped/completed worker(s) collapsed by default") {
		t.Fatalf("snapshot missing collapsed worker count: %q", output)
	}
	if !strings.Contains(output, "worker-stopped-1 state=stopped task=task-b reason=completed") {
		t.Fatalf("snapshot missing stopped worker summary: %q", output)
	}
}

func TestAnswerTUIQuestionPlan(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-ask-plan"
	manager := orchestrator.NewManager(base)
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Networks: []string{"192.168.50.0/24"}},
		Constraints:     []string{"internal_lab_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "task-1",
				Title:             "Seed reconnaissance",
				Goal:              "collect baseline",
				DoneWhen:          []string{"baseline"},
				FailWhen:          []string{"failed"},
				ExpectedArtifacts: []string{"seed.log"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget:            orchestrator.TaskBudget{MaxSteps: 2, MaxToolCalls: 2, MaxRuntime: time.Minute},
			},
		},
		Metadata: orchestrator.PlanMetadata{
			Goal: "Map network",
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}

	answer := answerTUIQuestion(manager, runID, "what is the complete plan?")
	if !strings.Contains(answer, "plan (1 steps):") {
		t.Fatalf("unexpected answer: %s", answer)
	}
	if !strings.Contains(answer, "1)Seed reconnaissance") {
		t.Fatalf("expected step title in answer: %s", answer)
	}
}

func TestAskInPlanningModeIsReadOnly(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-ask-read-only-planning"
	manager := orchestrator.NewManager(base)
	plan := buildInteractivePlanningPlan(
		runID,
		time.Now().UTC(),
		orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		2,
	)
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}

	scroll := 0
	if done, log := executeTUICommand(manager, runID, nil, tuiCommand{name: "ask", reason: "scan localhost services and summarize findings"}, &scroll); done || strings.TrimSpace(log) == "" || strings.Contains(strings.ToLower(log), "instruction queued") {
		t.Fatalf("expected ask response, got done=%v log=%q", done, log)
	}
	updated, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if len(updated.Tasks) != 0 {
		t.Fatalf("expected ask to be read-only in planning mode, got %d tasks", len(updated.Tasks))
	}
	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if hasEvent(events, orchestrator.EventTypeOperatorInstruction) {
		t.Fatalf("did not expect queued operator instruction from ask command")
	}
}

func TestBuildTUIAssistantContextIncludesReportPath(t *testing.T) {
	base := t.TempDir()
	runID := "run-tui-report-context"
	manager := orchestrator.NewManager(base)
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "scan localhost",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"scan.log"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     2,
					MaxToolCalls: 2,
					MaxRuntime:   2 * time.Second,
				},
			},
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	reportPath, err := manager.AssembleRunReport(runID, "")
	if err != nil {
		t.Fatalf("AssembleRunReport: %v", err)
	}
	if err := manager.EmitEvent(runID, "orchestrator", "", orchestrator.EventTypeRunReportGenerated, map[string]any{
		"path": reportPath,
	}); err != nil {
		t.Fatalf("emit run report event: %v", err)
	}
	contextText, err := buildTUIAssistantContext(manager, runID)
	if err != nil {
		t.Fatalf("buildTUIAssistantContext: %v", err)
	}
	if !strings.Contains(contextText, "latest_report_path="+reportPath) {
		t.Fatalf("expected report path in context:\n%s", contextText)
	}
	if !strings.Contains(contextText, "latest_report_ready=true") {
		t.Fatalf("expected report ready marker in context:\n%s", contextText)
	}
}

func mustPayload(t *testing.T, payload map[string]any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return data
}

func hasEvent(events []orchestrator.EventEnvelope, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
