package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

type fakePlannerChatClient struct {
	content string
	err     error
}

func (f fakePlannerChatClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	return llm.ChatResponse{Content: f.content}, nil
}

func TestRunStartStatusEventsStop(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan.json")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-1",
		Scope:           orchestrator.Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "scan",
				DoneWhen:          []string{"artifact"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"out.txt"},
				RiskLevel:         "active_probe",
				Budget: orchestrator.TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "run started: run-cli-1") {
		t.Fatalf("unexpected start output: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"status", "--sessions-dir", base, "--run", "run-cli-1", "--reclaim-startup"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("status failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "reclaimed_startup_leases: 0") {
		t.Fatalf("unexpected reclaim output: %q", out.String())
	}
	if !strings.Contains(out.String(), "state: running") {
		t.Fatalf("unexpected status output: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"events", "--sessions-dir", base, "--run", "run-cli-1"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("events failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"type":"run_started"`) {
		t.Fatalf("unexpected events output: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"stop", "--sessions-dir", base, "--run", "run-cli-1"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("stop failed: code=%d err=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"status", "--sessions-dir", base, "--run", "run-cli-1"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("status after stop failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "state: stopped") {
		t.Fatalf("unexpected status output: %q", out.String())
	}
}

func TestRunRejectsMissingRequiredFlags(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := run([]string{"start"}, &out, &errOut); code == 0 {
		t.Fatalf("expected start failure without --plan")
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"status"}, &out, &errOut); code == 0 {
		t.Fatalf("expected status failure without --run")
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"run", "--sessions-dir", base}, &out, &errOut); code != 0 {
		t.Fatalf("expected planning run bootstrap, got code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "run started in planning mode:") {
		t.Fatalf("unexpected planning run output: %q", out.String())
	}
}

func TestRunBootstrapPlanningModeWithoutGoal(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-cli-planning"
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run([]string{
		"run",
		"--sessions-dir", base,
		"--run", runID,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("run failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "run started in planning mode: "+runID) {
		t.Fatalf("unexpected output: %q", out.String())
	}

	manager := orchestrator.NewManager(base)
	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if phase := orchestrator.NormalizeRunPhase(plan.Metadata.RunPhase); phase != orchestrator.RunPhasePlanning {
		t.Fatalf("expected planning phase, got %q", plan.Metadata.RunPhase)
	}
	if len(plan.Tasks) != 0 {
		t.Fatalf("expected zero tasks in planning bootstrap, got %d", len(plan.Tasks))
	}

	events, err := manager.Events(runID, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	for _, event := range events {
		if event.Type == orchestrator.EventTypeWorkerStarted || event.Type == orchestrator.EventTypeTaskStarted {
			t.Fatalf("unexpected execution event in planning bootstrap: %s", event.Type)
		}
	}
}

func TestRunGoalModeRejectsMissingScopeOrConstraints(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	var out bytes.Buffer
	var errOut bytes.Buffer

	args := []string{
		"run",
		"--sessions-dir", base,
		"--goal", "check target",
		"--constraint", "internal_only",
		"--worker-cmd", os.Args[0],
	}
	if code := run(args, &out, &errOut); code == 0 {
		t.Fatalf("expected goal run failure without scope")
	}
	if !strings.Contains(errOut.String(), "--scope-target or --scope-network") {
		t.Fatalf("unexpected missing-scope error: %q", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	args = []string{
		"run",
		"--sessions-dir", base,
		"--goal", "check target",
		"--scope-target", "127.0.0.1",
		"--worker-cmd", os.Args[0],
	}
	if code := run(args, &out, &errOut); code == 0 {
		t.Fatalf("expected goal run failure without constraints")
	}
	if !strings.Contains(errOut.String(), "--constraint") {
		t.Fatalf("unexpected missing-constraint error: %q", errOut.String())
	}
}

func TestRunGoalModeSeedsPlanAndPersistsMetadata(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-cli-goal-seed"
	var out bytes.Buffer
	var errOut bytes.Buffer
	args := []string{
		"run",
		"--sessions-dir", base,
		"--run", runID,
		"--goal", "  investigate    host   exposure  ",
		"--scope-target", "127.0.0.1",
		"--constraint", "local_only",
		"--permissions", "all",
		"--success-criterion", "goal_processed",
		"--stop-criterion", "manual_stop",
		"--worker-cmd", os.Args[0],
		"--worker-arg", "-test.run=TestHelperProcessOrchestratorWorker",
		"--worker-arg", "worker-ok",
		"--worker-env", "GO_WANT_HELPER_PROCESS=1",
		"--tick", "20ms",
		"--startup-timeout", "1s",
		"--stale-timeout", "1s",
		"--soft-stall-grace", "1s",
	}
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("goal run failed: code=%d err=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "run started: "+runID) || !strings.Contains(output, "run completed: "+runID) {
		t.Fatalf("unexpected run output: %q", output)
	}

	plan, err := orchestrator.NewManager(base).LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if plan.Metadata.Goal != "investigate host exposure" {
		t.Fatalf("unexpected metadata goal: %q", plan.Metadata.Goal)
	}
	if plan.Metadata.NormalizedGoal != "investigate host exposure" {
		t.Fatalf("unexpected normalized goal: %q", plan.Metadata.NormalizedGoal)
	}
	if plan.Metadata.PlannerMode != "goal_seed_v1" {
		t.Fatalf("unexpected planner mode: %q", plan.Metadata.PlannerMode)
	}
	if strings.TrimSpace(plan.Metadata.PlannerVersion) == "" {
		t.Fatalf("expected planner version in metadata")
	}
	if strings.TrimSpace(plan.Metadata.PlannerPromptHash) == "" {
		t.Fatalf("expected planner prompt hash in metadata")
	}
	if plan.Metadata.PlannerDecision != "approve" {
		t.Fatalf("expected approve planner decision, got %q", plan.Metadata.PlannerDecision)
	}
	if phase := orchestrator.NormalizeRunPhase(plan.Metadata.RunPhase); phase != orchestrator.RunPhaseCompleted {
		t.Fatalf("expected completed run phase after execution, got %q", plan.Metadata.RunPhase)
	}
	if len(plan.Metadata.Hypotheses) == 0 {
		t.Fatalf("expected generated hypotheses in plan metadata")
	}
	if len(plan.Metadata.Hypotheses[0].EvidenceRequired) == 0 {
		t.Fatalf("expected hypothesis evidence requirements")
	}
	if len(plan.Constraints) != 1 || plan.Constraints[0] != "local_only" {
		t.Fatalf("unexpected constraints: %#v", plan.Constraints)
	}
	if len(plan.Scope.Targets) != 1 || plan.Scope.Targets[0] != "127.0.0.1" {
		t.Fatalf("unexpected scope targets: %#v", plan.Scope.Targets)
	}
	if len(plan.Tasks) < 3 {
		t.Fatalf("expected synthesized task graph, got %#v", plan.Tasks)
	}
	if plan.Tasks[0].TaskID != "task-recon-seed" {
		t.Fatalf("expected recon seed as first task, got %s", plan.Tasks[0].TaskID)
	}
	if plan.Tasks[len(plan.Tasks)-1].TaskID != "task-plan-summary" {
		t.Fatalf("expected summary task as terminal node, got %s", plan.Tasks[len(plan.Tasks)-1].TaskID)
	}
	if len(plan.Tasks[0].ExpectedArtifacts) == 0 || len(plan.Tasks[len(plan.Tasks)-1].ExpectedArtifacts) == 0 {
		t.Fatalf("unexpected seed tasks: %#v", plan.Tasks)
	}
}

func TestRunGoalModeRejectsInvalidPlannerMode(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	var out bytes.Buffer
	var errOut bytes.Buffer
	args := []string{
		"run",
		"--sessions-dir", base,
		"--goal", "investigate host exposure",
		"--scope-target", "127.0.0.1",
		"--constraint", "local_only",
		"--planner", "invalid",
		"--plan-review", "reject",
	}
	if code := run(args, &out, &errOut); code == 0 {
		t.Fatalf("expected planner mode validation failure")
	}
	if !strings.Contains(errOut.String(), "invalid --planner") {
		t.Fatalf("unexpected planner mode error output: %q", errOut.String())
	}
}

func TestBuildGoalLLMPlanUsesLLMSynthesizedGraph(t *testing.T) {
	t.Parallel()

	fake := fakePlannerChatClient{
		content: `{
			"rationale":"split recon and summarize",
			"tasks":[
				{
					"task_id":"task-llm-recon",
					"title":"LLM Recon",
					"goal":"Collect passive recon evidence",
					"targets":["192.168.50.0/24"],
					"depends_on":[],
					"priority":100,
					"strategy":"recon_seed",
					"risk_level":"recon_readonly",
					"done_when":["recon_complete"],
					"fail_when":["recon_failed"],
					"expected_artifacts":["recon.log"],
					"action":{"type":"assist","prompt":"run passive recon"},
					"budget":{"max_steps":8,"max_tool_calls":12,"max_runtime_seconds":240}
				},
				{
					"task_id":"task-llm-summary",
					"title":"LLM Summary",
					"goal":"Summarize findings",
					"targets":["192.168.50.0/24"],
					"depends_on":["task-llm-recon"],
					"priority":10,
					"strategy":"summarize_and_replan",
					"risk_level":"recon_readonly",
					"done_when":["summary_done"],
					"fail_when":["summary_failed"],
					"expected_artifacts":["summary.log"],
					"action":{"type":"assist","prompt":"summarize recon findings"},
					"budget":{"max_steps":4,"max_tool_calls":6,"max_runtime_seconds":180}
				}
			]
		}`,
	}

	plan, note, err := buildGoalLLMPlan(
		context.Background(),
		config.Config{},
		fake,
		"qwen/qwen3-coder-30b",
		"run-llm-plan",
		"map services",
		orchestrator.Scope{Networks: []string{"192.168.50.0/24"}},
		[]string{"internal_lab_only"},
		nil,
		nil,
		2,
		5,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("buildGoalLLMPlan: %v", err)
	}
	if plan.Metadata.PlannerMode != plannerModeLLMV1 {
		t.Fatalf("unexpected planner mode: %q", plan.Metadata.PlannerMode)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 llm tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[1].DependsOn[0] != "task-llm-recon" {
		t.Fatalf("unexpected llm dependency: %#v", plan.Tasks[1].DependsOn)
	}
	if strings.TrimSpace(note) == "" {
		t.Fatalf("expected llm planner rationale note")
	}
}

func TestBuildGoalPlanFromModeAutoFallsBackToStaticWhenLLMUnavailable(t *testing.T) {
	t.Parallel()

	plan, note, err := buildGoalPlanFromMode(
		context.Background(),
		"auto",
		"",
		"run-auto-fallback",
		"map services",
		orchestrator.Scope{Networks: []string{"192.168.50.0/24"}},
		[]string{"internal_lab_only"},
		nil,
		nil,
		2,
		5,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("buildGoalPlanFromMode(auto): %v", err)
	}
	if plan.Metadata.PlannerMode != plannerModeStaticV1 {
		t.Fatalf("expected static fallback mode, got %q", plan.Metadata.PlannerMode)
	}
	if !strings.Contains(note, "fallback") {
		t.Fatalf("expected fallback note, got %q", note)
	}
}

func TestRunGoalModePlanReviewRejectWritesReviewArtifact(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-cli-goal-reject"
	var out bytes.Buffer
	var errOut bytes.Buffer
	args := []string{
		"run",
		"--sessions-dir", base,
		"--run", runID,
		"--goal", "investigate host exposure",
		"--scope-target", "127.0.0.1",
		"--constraint", "local_only",
		"--plan-review", "reject",
		"--plan-review-rationale", "operator rejected generated plan",
	}
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("goal reject run failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "plan rejected:") {
		t.Fatalf("expected rejected output, got %q", out.String())
	}

	planPath := filepath.Join(base, runID, "orchestrator", "plan", "plan.review.json")
	if _, err := os.Stat(planPath); err != nil {
		t.Fatalf("expected rejected plan artifact: %v", err)
	}
	reviewPath := filepath.Join(base, runID, "orchestrator", "plan", "plan.review.audit.json")
	data, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatalf("read review artifact: %v", err)
	}
	if !strings.Contains(string(data), `"decision": "reject"`) {
		t.Fatalf("expected reject decision in review artifact: %s", string(data))
	}
}

func TestRunGoalModePlanReviewEditWritesDraft(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-cli-goal-edit"
	var out bytes.Buffer
	var errOut bytes.Buffer
	args := []string{
		"run",
		"--sessions-dir", base,
		"--run", runID,
		"--goal", "investigate host exposure",
		"--scope-target", "127.0.0.1",
		"--constraint", "local_only",
		"--plan-review", "edit",
	}
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("goal edit run failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "plan draft written:") {
		t.Fatalf("expected draft output, got %q", out.String())
	}

	draftPath := filepath.Join(base, runID, "orchestrator", "plan", "plan.draft.json")
	if _, err := os.Stat(draftPath); err != nil {
		t.Fatalf("expected editable draft plan artifact: %v", err)
	}
	reviewPath := filepath.Join(base, runID, "orchestrator", "plan", "plan.review.audit.json")
	if _, err := os.Stat(reviewPath); err != nil {
		t.Fatalf("expected review audit artifact: %v", err)
	}
}

func TestRunCommandExecutesPlan(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-run.json")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-loop",
		Scope:           orchestrator.Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "collect",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"out.txt"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	args := []string{
		"run",
		"--sessions-dir", base,
		"--run", "run-cli-loop",
		"--worker-cmd", os.Args[0],
		"--worker-arg", "-test.run=TestHelperProcessOrchestratorWorker",
		"--worker-arg", "worker-ok",
		"--worker-env", "GO_WANT_HELPER_PROCESS=1",
		"--tick", "20ms",
		"--startup-timeout", "1s",
		"--stale-timeout", "1s",
		"--soft-stall-grace", "1s",
	}
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("run failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "run completed: run-cli-loop") {
		t.Fatalf("unexpected run output: %q", out.String())
	}
}

func TestRunCommandExecutesProductionWorkerMode(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available in PATH")
	}

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-run-worker-mode.json")
	cmdName, cmdArgs := workerShellEchoCommand("worker-mode-plan-ok")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-worker-mode",
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "run production worker",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"command_log"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Action: orchestrator.TaskAction{
					Type:    "command",
					Command: cmdName,
					Args:    cmdArgs,
				},
				Budget: orchestrator.TaskBudget{
					MaxSteps:     2,
					MaxToolCalls: 2,
					MaxRuntime:   5 * time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	repoRoot := testRepoRoot(t)
	binPath := buildBirdHackBotBinary(t, repoRoot, base)

	out.Reset()
	errOut.Reset()
	args := []string{
		"run",
		"--sessions-dir", base,
		"--run", "run-cli-worker-mode",
		"--worker-cmd", binPath,
		"--worker-arg", "worker",
		"--tick", "20ms",
		"--startup-timeout", "1s",
		"--stale-timeout", "1s",
		"--soft-stall-grace", "1s",
		"--max-attempts", "1",
	}
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("run failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "run completed: run-cli-worker-mode") {
		t.Fatalf("unexpected run output: %q", out.String())
	}

	events, err := orchestrator.NewManager(base).Events("run-cli-worker-mode", 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, orchestrator.EventTypeTaskArtifact) {
		t.Fatalf("expected task_artifact event from worker mode")
	}
	if !hasEventType(events, orchestrator.EventTypeTaskFinding) {
		t.Fatalf("expected task_finding event from worker mode")
	}
}

func TestRunToReportProductionWorkerMode(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available in PATH")
	}

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-run-report-worker-mode.json")
	cmdName, cmdArgs := workerShellEchoCommand("worker-mode-report-ok")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-worker-report",
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "run production worker and report",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"command_log"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Action: orchestrator.TaskAction{
					Type:    "command",
					Command: cmdName,
					Args:    cmdArgs,
				},
				Budget: orchestrator.TaskBudget{
					MaxSteps:     2,
					MaxToolCalls: 2,
					MaxRuntime:   5 * time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	repoRoot := testRepoRoot(t)
	binPath := buildBirdHackBotBinary(t, repoRoot, base)

	out.Reset()
	errOut.Reset()
	args := []string{
		"run",
		"--sessions-dir", base,
		"--run", "run-cli-worker-report",
		"--worker-cmd", binPath,
		"--worker-arg", "worker",
		"--tick", "20ms",
		"--startup-timeout", "1s",
		"--stale-timeout", "1s",
		"--soft-stall-grace", "1s",
		"--max-attempts", "1",
	}
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("run failed: code=%d err=%s", code, errOut.String())
	}

	reportPath := filepath.Join(base, "report.md")
	out.Reset()
	errOut.Reset()
	if code := run([]string{"report", "--sessions-dir", base, "--run", "run-cli-worker-report", "--out", reportPath}, &out, &errOut); code != 0 {
		t.Fatalf("report failed: code=%d err=%s", code, errOut.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(data)), "task action completed") {
		t.Fatalf("expected report to include worker finding")
	}
}

func TestRunApprovalToCompletionProductionWorkerMode(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available in PATH")
	}

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-run-approval-worker-mode.json")
	cmdName, cmdArgs := workerShellEchoCommand("worker-mode-approval-ok")
	runID := "run-cli-worker-approval"
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
				Goal:              "run production worker approval flow",
				Targets:           []string{"127.0.0.1"},
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"command_log"},
				RiskLevel:         string(orchestrator.RiskActiveProbe),
				Action: orchestrator.TaskAction{
					Type:    "command",
					Command: cmdName,
					Args:    cmdArgs,
				},
				Budget: orchestrator.TaskBudget{
					MaxSteps:     2,
					MaxToolCalls: 2,
					MaxRuntime:   5 * time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	repoRoot := testRepoRoot(t)
	binPath := buildBirdHackBotBinary(t, repoRoot, base)

	runArgs := []string{
		"run",
		"--sessions-dir", base,
		"--run", runID,
		"--worker-cmd", binPath,
		"--worker-arg", "worker",
		"--permissions", "default",
		"--tick", "20ms",
		"--startup-timeout", "1s",
		"--stale-timeout", "1s",
		"--soft-stall-grace", "1s",
		"--max-attempts", "1",
	}
	var runOut bytes.Buffer
	var runErr bytes.Buffer
	codeCh := make(chan int, 1)
	go func() {
		codeCh <- run(runArgs, &runOut, &runErr)
	}()

	manager := orchestrator.NewManager(base)
	approvalID := waitForApprovalID(t, manager, runID, 5*time.Second)

	var approveOut bytes.Buffer
	var approveErr bytes.Buffer
	if code := run([]string{
		"approve",
		"--sessions-dir", base,
		"--run", runID,
		"--approval", approvalID,
		"--scope", "task",
		"--actor", "tester",
		"--reason", "approved for test",
	}, &approveOut, &approveErr); code != 0 {
		t.Fatalf("approve failed: code=%d err=%s", code, approveErr.String())
	}

	select {
	case code := <-codeCh:
		if code != 0 {
			t.Fatalf("run failed: code=%d err=%s", code, runErr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for run completion")
	}

	reportPath := filepath.Join(base, "run-approval-report.md")
	var reportOut bytes.Buffer
	var reportErr bytes.Buffer
	if code := run([]string{"report", "--sessions-dir", base, "--run", runID, "--out", reportPath}, &reportOut, &reportErr); code != 0 {
		t.Fatalf("report failed: code=%d err=%s", code, reportErr.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(data)), "task action completed") {
		t.Fatalf("expected report to include worker completion finding")
	}
}

func TestApprovalCommands(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-approval.json")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-approval",
		Scope:           orchestrator.Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "scan",
				DoneWhen:          []string{"artifact"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"out.txt"},
				RiskLevel:         string(orchestrator.RiskActiveProbe),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	manager := orchestrator.NewManager(base)
	if err := manager.EmitEvent("run-cli-approval", "orchestrator", "t1", orchestrator.EventTypeApprovalRequested, map[string]any{
		"approval_id": "apr-1",
		"tier":        string(orchestrator.RiskActiveProbe),
		"reason":      "requires approval",
		"expires_at":  time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("EmitEvent approval requested: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"approvals", "--sessions-dir", base, "--run", "run-cli-approval"}, &out, &errOut); code != 0 {
		t.Fatalf("approvals failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"approval_id":"apr-1"`) {
		t.Fatalf("unexpected approvals output: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"approve", "--sessions-dir", base, "--run", "run-cli-approval", "--approval", "apr-1", "--scope", "task", "--actor", "tester", "--reason", "ok"}, &out, &errOut); code != 0 {
		t.Fatalf("approve failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "approved: apr-1") {
		t.Fatalf("unexpected approve output: %q", out.String())
	}
}

func TestWorkerStopCommand(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-worker-stop.json")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-worker-stop",
		Scope:           orchestrator.Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "scan",
				DoneWhen:          []string{"artifact"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"out.txt"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"worker-stop", "--sessions-dir", base, "--run", "run-cli-worker-stop", "--worker", "worker-t1-a1", "--actor", "tester", "--reason", "manual stop"}, &out, &errOut); code != 0 {
		t.Fatalf("worker-stop failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "worker stop requested") {
		t.Fatalf("unexpected worker-stop output: %q", out.String())
	}

	events, err := orchestrator.NewManager(base).Events("run-cli-worker-stop", 0)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Type == orchestrator.EventTypeWorkerStopRequested {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected worker_stop_requested event")
	}
}

func TestReportCommand(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-report.json")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-report",
		Scope:           orchestrator.Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "scan",
				DoneWhen:          []string{"artifact"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"out.txt"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	manager := orchestrator.NewManager(base)
	if err := manager.EmitEvent("run-cli-report", "worker-1", "t1", orchestrator.EventTypeTaskArtifact, map[string]any{
		"type":  "log",
		"title": "scan output",
		"path":  "sessions/run-cli-report/logs/scan.log",
	}); err != nil {
		t.Fatalf("emit artifact: %v", err)
	}
	if err := manager.EmitEvent("run-cli-report", "worker-1", "t1", orchestrator.EventTypeTaskFinding, map[string]any{
		"target":       "192.168.50.10",
		"finding_type": "open_port",
		"title":        "SSH open",
		"location":     "22/tcp",
		"severity":     "low",
		"confidence":   "high",
	}); err != nil {
		t.Fatalf("emit finding: %v", err)
	}
	if _, err := manager.IngestEvidence("run-cli-report"); err != nil {
		t.Fatalf("ingest evidence: %v", err)
	}

	out.Reset()
	errOut.Reset()
	reportPath := filepath.Join(base, "out-report.md")
	if code := run([]string{"report", "--sessions-dir", base, "--run", "run-cli-report", "--out", reportPath}, &out, &errOut); code != 0 {
		t.Fatalf("report failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "report written:") {
		t.Fatalf("unexpected report output: %q", out.String())
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report file at %s: %v", reportPath, err)
	}
}

func TestStatusIncludesReportPathAndReadyFlag(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-status-report.json")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-status-report",
		Scope:           orchestrator.Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "scan",
				DoneWhen:          []string{"artifact"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"out.txt"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"status", "--sessions-dir", base, "--run", "run-cli-status-report"}, &out, &errOut); code != 0 {
		t.Fatalf("status failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "report_path:") || !strings.Contains(out.String(), "report_ready: false") {
		t.Fatalf("expected report discoverability in status output: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"report", "--sessions-dir", base, "--run", "run-cli-status-report"}, &out, &errOut); code != 0 {
		t.Fatalf("report failed: code=%d err=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"status", "--sessions-dir", base, "--run", "run-cli-status-report"}, &out, &errOut); code != 0 {
		t.Fatalf("status after report failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "report_ready: true") {
		t.Fatalf("expected report_ready true after generation: %q", out.String())
	}
}

func TestRunEndToEndLifecycleFanoutEvidenceAndReport(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-e2e.json")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-e2e",
		Scope:           orchestrator.Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  2,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "collect-one",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"t1.log"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     10,
					MaxToolCalls: 10,
					MaxRuntime:   30 * time.Second,
				},
			},
			{
				TaskID:            "t2",
				Goal:              "collect-two",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"t2.log"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     10,
					MaxToolCalls: 10,
					MaxRuntime:   30 * time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	runArgs := []string{
		"run",
		"--sessions-dir", base,
		"--run", "run-cli-e2e",
		"--worker-cmd", os.Args[0],
		"--worker-arg", "-test.run=TestHelperProcessOrchestratorWorker",
		"--worker-arg", "worker-evidence",
		"--worker-env", "GO_WANT_HELPER_PROCESS=1",
		"--worker-env", "TEST_SESSIONS_DIR=" + base,
		"--tick", "20ms",
		"--startup-timeout", "2s",
		"--stale-timeout", "2s",
		"--soft-stall-grace", "2s",
	}
	if code := run(runArgs, &out, &errOut); code != 0 {
		t.Fatalf("run failed: code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "run completed: run-cli-e2e") {
		t.Fatalf("unexpected run output: %q", out.String())
	}

	manager := orchestrator.NewManager(base)
	status, err := manager.Status("run-cli-e2e")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != "completed" {
		t.Fatalf("expected completed run state, got %s", status.State)
	}
	findings, err := manager.ListFindings("run-cli-e2e")
	if err != nil {
		t.Fatalf("ListFindings: %v", err)
	}
	if len(findings) < 2 {
		t.Fatalf("expected merged findings from fan-out workers, got %d", len(findings))
	}
	events, err := manager.Events("run-cli-e2e", 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasEventType(events, orchestrator.EventTypeRunReportGenerated) {
		t.Fatalf("expected run_report_generated event in terminal outcome")
	}

	out.Reset()
	errOut.Reset()
	reportPath := filepath.Join(base, "run-e2e-report.md")
	if code := run([]string{"report", "--sessions-dir", base, "--run", "run-cli-e2e", "--out", reportPath}, &out, &errOut); code != 0 {
		t.Fatalf("report failed: code=%d err=%s", code, errOut.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "finding-t1") || !strings.Contains(content, "finding-t2") {
		t.Fatalf("report missing fan-out finding details:\n%s", content)
	}
}

func TestRunBudgetGuardStopsOnStepExhaustion(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	planPath := filepath.Join(base, "plan-budget.json")
	plan := orchestrator.RunPlan{
		RunID:           "run-cli-budget",
		Scope:           orchestrator.Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []orchestrator.TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "budget-test",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"timeout"},
				ExpectedArtifacts: []string{"none"},
				RiskLevel:         string(orchestrator.RiskReconReadonly),
				Budget: orchestrator.TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   30 * time.Second,
				},
			},
		},
	}
	writePlanFile(t, planPath, plan)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"start", "--sessions-dir", base, "--plan", planPath}, &out, &errOut); code != 0 {
		t.Fatalf("start failed: code=%d err=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	runArgs := []string{
		"run",
		"--sessions-dir", base,
		"--run", "run-cli-budget",
		"--worker-cmd", os.Args[0],
		"--worker-arg", "-test.run=TestHelperProcessOrchestratorWorker",
		"--worker-arg", "worker-budget-steps",
		"--worker-env", "GO_WANT_HELPER_PROCESS=1",
		"--worker-env", "TEST_SESSIONS_DIR=" + base,
		"--tick", "20ms",
		"--startup-timeout", "2s",
		"--stale-timeout", "2s",
		"--soft-stall-grace", "2s",
	}
	if code := run(runArgs, &out, &errOut); code == 0 {
		t.Fatalf("expected non-zero run exit for failed terminal state")
	}
	if !strings.Contains(out.String(), "run stopped with failures: run-cli-budget") {
		t.Fatalf("unexpected run output: %q", out.String())
	}

	manager := orchestrator.NewManager(base)
	events, err := manager.Events("run-cli-budget", 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if !hasTaskFailedReason(events, "budget_exhausted") {
		t.Fatalf("expected task_failed with budget_exhausted reason")
	}
	if !hasReplanTrigger(events, "budget_exhausted") {
		t.Fatalf("expected replan trigger for budget_exhausted")
	}
}

func TestRunGoalToGeneratedPlanApprovalFanoutAndReport(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-cli-goal-e2e"
	runArgs := []string{
		"run",
		"--sessions-dir", base,
		"--run", runID,
		"--goal", "scan network services and assess web auth surface",
		"--scope-target", "127.0.0.1",
		"--constraint", "local_only",
		"--permissions", "default",
		"--worker-cmd", os.Args[0],
		"--worker-arg", "-test.run=TestHelperProcessOrchestratorWorker",
		"--worker-arg", "worker-evidence",
		"--worker-env", "GO_WANT_HELPER_PROCESS=1",
		"--worker-env", "TEST_SESSIONS_DIR=" + base,
		"--tick", "20ms",
		"--startup-timeout", "2s",
		"--stale-timeout", "2s",
		"--soft-stall-grace", "2s",
		"--max-attempts", "1",
		"--plan-review", "approve",
		"--max-parallelism", "2",
	}
	var runOut bytes.Buffer
	var runErr bytes.Buffer
	codeCh := make(chan int, 1)
	go func() {
		codeCh <- run(runArgs, &runOut, &runErr)
	}()

	manager := orchestrator.NewManager(base)
	autoApprovePending(t, manager, runID, 10*time.Second)

	select {
	case code := <-codeCh:
		if code != 0 {
			t.Fatalf("goal run failed: code=%d err=%s", code, runErr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("timed out waiting for goal run completion")
	}
	if !strings.Contains(runOut.String(), "run completed: "+runID) {
		t.Fatalf("unexpected goal run output: %s", runOut.String())
	}

	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if len(plan.Metadata.Hypotheses) == 0 {
		t.Fatalf("expected hypotheses in generated goal plan metadata")
	}

	reportPath := filepath.Join(base, "goal-e2e-report.md")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := run([]string{"report", "--sessions-dir", base, "--run", runID, "--out", reportPath}, &out, &errOut); code != 0 {
		t.Fatalf("report failed: code=%d err=%s", code, errOut.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(data)), "findings") {
		t.Fatalf("expected findings section in report")
	}
}

func TestHelperProcessOrchestratorWorker(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	mode := ""
	for _, arg := range os.Args {
		switch arg {
		case "worker-ok", "worker-evidence", "worker-budget-steps", "worker-zip-crack", "worker-zip-unsolved":
			mode = arg
		}
	}
	switch mode {
	case "worker-ok":
		os.Exit(0)
	case "worker-evidence":
		if err := helperEmitEvidence(); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(2)
		}
		os.Exit(0)
	case "worker-budget-steps":
		if err := helperEmitBudgetOverrun(); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(2)
		}
		time.Sleep(10 * time.Second)
		os.Exit(0)
	case "worker-zip-crack":
		if err := helperZipCrackScenario(true); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(2)
		}
		os.Exit(0)
	case "worker-zip-unsolved":
		if err := helperZipCrackScenario(false); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(1)
}

func writePlanFile(t *testing.T, path string, plan orchestrator.RunPlan) {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
}

func helperEmitEvidence() error {
	manager, runID, taskID, workerID, err := helperManagerAndIDs()
	if err != nil {
		return err
	}
	signalWorker := "signal-" + workerID
	if err := manager.EmitEvent(runID, signalWorker, taskID, orchestrator.EventTypeTaskArtifact, map[string]any{
		"type":  "log",
		"title": "artifact-" + taskID,
		"path":  filepath.ToSlash(filepath.Join("sessions", runID, "logs", taskID+".log")),
	}); err != nil {
		return err
	}
	return manager.EmitEvent(runID, signalWorker, taskID, orchestrator.EventTypeTaskFinding, map[string]any{
		"target":       "192.168.50.10",
		"finding_type": "service",
		"title":        "finding-" + taskID,
		"location":     taskID,
		"severity":     "low",
		"confidence":   "high",
		"source":       "helper-worker",
		"evidence":     []string{"evidence-" + taskID},
	})
}

func helperEmitBudgetOverrun() error {
	manager, runID, taskID, workerID, err := helperManagerAndIDs()
	if err != nil {
		return err
	}
	return manager.EmitEvent(runID, "signal-"+workerID, taskID, orchestrator.EventTypeTaskProgress, map[string]any{
		"message":    "simulating high-step task",
		"steps":      5,
		"tool_calls": 2,
	})
}

func helperManagerAndIDs() (*orchestrator.Manager, string, string, string, error) {
	sessionsDir := strings.TrimSpace(os.Getenv("TEST_SESSIONS_DIR"))
	runID := strings.TrimSpace(os.Getenv("BIRDHACKBOT_ORCH_RUN_ID"))
	taskID := strings.TrimSpace(os.Getenv("BIRDHACKBOT_ORCH_TASK_ID"))
	workerID := strings.TrimSpace(os.Getenv("BIRDHACKBOT_ORCH_WORKER_ID"))
	if sessionsDir == "" || runID == "" || taskID == "" || workerID == "" {
		return nil, "", "", "", os.ErrInvalid
	}
	return orchestrator.NewManager(sessionsDir), runID, taskID, workerID, nil
}

func hasTaskFailedReason(events []orchestrator.EventEnvelope, reason string) bool {
	for _, event := range events {
		if event.Type != orchestrator.EventTypeTaskFailed {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if strings.TrimSpace(payloadString(payload["reason"])) == reason {
			return true
		}
	}
	return false
}

func hasReplanTrigger(events []orchestrator.EventEnvelope, trigger string) bool {
	for _, event := range events {
		if event.Type != orchestrator.EventTypeRunReplanRequested {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if strings.TrimSpace(payloadString(payload["trigger"])) == trigger {
			return true
		}
	}
	return false
}

func payloadString(v any) string {
	s, _ := v.(string)
	return s
}

func hasEventType(events []orchestrator.EventEnvelope, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func workerShellEchoCommand(message string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", "echo " + message}
	}
	return "sh", []string{"-c", "printf '%s\\n' " + message}
}

func buildBirdHackBotBinary(t *testing.T, repoRoot, outDir string) string {
	t.Helper()
	binPath := filepath.Join(outDir, "birdhackbot")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/birdhackbot")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build birdhackbot: %v\n%s", err, string(output))
	}
	return binPath
}

func waitForApprovalID(t *testing.T, manager *orchestrator.Manager, runID string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pending, err := manager.PendingApprovals(runID)
		if err == nil && len(pending) > 0 && strings.TrimSpace(pending[0].ApprovalID) != "" {
			return pending[0].ApprovalID
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pending approval on run %s", runID)
	return ""
}

func autoApprovePending(t *testing.T, manager *orchestrator.Manager, runID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	approved := map[string]struct{}{}
	for time.Now().Before(deadline) {
		status, err := manager.Status(runID)
		if err == nil && (status.State == "completed" || status.State == "stopped") {
			return
		}
		pending, err := manager.PendingApprovals(runID)
		if err == nil {
			for _, req := range pending {
				if req.ApprovalID == "" {
					continue
				}
				if _, seen := approved[req.ApprovalID]; seen {
					continue
				}
				if err := manager.SubmitApprovalDecision(runID, req.ApprovalID, true, "task", "auto-test", "goal-e2e auto approve", 0); err != nil {
					t.Fatalf("SubmitApprovalDecision(%s): %v", req.ApprovalID, err)
				}
				approved[req.ApprovalID] = struct{}{}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
