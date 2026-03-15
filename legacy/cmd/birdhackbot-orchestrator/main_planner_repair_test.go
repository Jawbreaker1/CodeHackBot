package main

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestMaybeRepairPlannerPreflightFailureRepairsCycle(t *testing.T) {
	t.Parallel()

	plan := plannerRepairPlan([]orchestrator.TaskSpec{
		plannerRepairTask("task-a", []string{"task-c"}),
		plannerRepairTask("task-b", []string{"task-a"}),
		plannerRepairTask("task-c", []string{"task-b"}),
	})
	err := orchestrator.ValidateSynthesizedPlan(plan)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "cycle detected") {
		t.Fatalf("expected cycle preflight error, got %v", err)
	}

	repaired, note, ok := maybeRepairPlannerPreflightFailure(plan, err)
	if !ok {
		t.Fatalf("expected planner repair to succeed")
	}
	if !strings.Contains(note, "auto-repaired") {
		t.Fatalf("expected repair note, got %q", note)
	}
	if err := orchestrator.ValidateSynthesizedPlan(repaired); err != nil {
		t.Fatalf("repaired plan should validate: %v", err)
	}
	if len(repaired.Tasks[0].DependsOn) != 0 {
		t.Fatalf("expected task-a deps pruned, got %v", repaired.Tasks[0].DependsOn)
	}
}

func TestMaybeRepairPlannerPreflightFailureRepairsUnknownDependencies(t *testing.T) {
	t.Parallel()

	plan := plannerRepairPlan([]orchestrator.TaskSpec{
		plannerRepairTask("task-1", nil),
		plannerRepairTask("task-2", []string{"task-missing", "task-1", "task-1"}),
	})
	err := orchestrator.ValidateSynthesizedPlan(plan)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unknown task") {
		t.Fatalf("expected unknown dependency preflight error, got %v", err)
	}

	repaired, _, ok := maybeRepairPlannerPreflightFailure(plan, err)
	if !ok {
		t.Fatalf("expected planner repair to handle unknown dependency")
	}
	if err := orchestrator.ValidateSynthesizedPlan(repaired); err != nil {
		t.Fatalf("repaired plan should validate: %v", err)
	}
	if got := repaired.Tasks[1].DependsOn; len(got) != 1 || got[0] != "task-1" {
		t.Fatalf("expected unknown/duplicate deps removed, got %v", got)
	}
}

func TestBuildGoalPlanFromModeAutoRepairsCycleWithoutExtraRetry(t *testing.T) {
	t.Setenv("BIRDHACKBOT_CONFIG_PATH", filepath.Join(testRepoRoot(t), "config", "default.json"))
	callCount := 0
	server := newHTTPTestServerOrSkip(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"rationale\":\"cycle candidate\",\"tasks\":[{\"task_id\":\"task-a\",\"title\":\"A\",\"goal\":\"a\",\"targets\":[\"127.0.0.1\"],\"depends_on\":[\"task-c\"],\"priority\":90,\"strategy\":\"recon_seed\",\"risk_level\":\"recon_readonly\",\"done_when\":[\"a_done\"],\"fail_when\":[\"a_fail\"],\"expected_artifacts\":[\"a.log\"],\"action\":{\"type\":\"assist\",\"prompt\":\"do a\"},\"budget\":{\"max_steps\":8,\"max_tool_calls\":12,\"max_runtime_seconds\":120}},{\"task_id\":\"task-b\",\"title\":\"B\",\"goal\":\"b\",\"targets\":[\"127.0.0.1\"],\"depends_on\":[\"task-a\"],\"priority\":80,\"strategy\":\"service_scan\",\"risk_level\":\"recon_readonly\",\"done_when\":[\"b_done\"],\"fail_when\":[\"b_fail\"],\"expected_artifacts\":[\"b.log\"],\"action\":{\"type\":\"assist\",\"prompt\":\"do b\"},\"budget\":{\"max_steps\":8,\"max_tool_calls\":12,\"max_runtime_seconds\":120}},{\"task_id\":\"task-c\",\"title\":\"C\",\"goal\":\"c\",\"targets\":[\"127.0.0.1\"],\"depends_on\":[\"task-b\"],\"priority\":70,\"strategy\":\"summary\",\"risk_level\":\"recon_readonly\",\"done_when\":[\"c_done\"],\"fail_when\":[\"c_fail\"],\"expected_artifacts\":[\"c.log\"],\"action\":{\"type\":\"assist\",\"prompt\":\"do c\"},\"budget\":{\"max_steps\":8,\"max_tool_calls\":12,\"max_runtime_seconds\":120}}]}"}}]}`))
	}))
	defer server.Close()
	t.Setenv(plannerLLMBaseURLEnv, server.URL)
	t.Setenv(plannerLLMModelEnv, "mock-planner-model")
	t.Setenv(plannerLLMAPIKeyEnv, "")

	plan, note, err := buildGoalPlanFromMode(
		context.Background(),
		"auto",
		"",
		t.TempDir(),
		"run-auto-cycle-repair",
		"map services",
		orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		[]string{"local_only"},
		nil,
		nil,
		1,
		5,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("buildGoalPlanFromMode(auto): %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected no extra retry once repair is possible, got %d calls", callCount)
	}
	if !strings.Contains(note, "auto-repaired") {
		t.Fatalf("expected repair note in planner rationale, got %q", note)
	}
	if err := orchestrator.ValidateSynthesizedPlan(plan); err != nil {
		t.Fatalf("expected repaired plan to validate, got %v", err)
	}
}

func plannerRepairPlan(tasks []orchestrator.TaskSpec) orchestrator.RunPlan {
	return orchestrator.RunPlan{
		RunID:           "run-planner-repair",
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"manual_stop"},
		MaxParallelism:  1,
		Tasks:           tasks,
	}
}

func plannerRepairTask(id string, deps []string) orchestrator.TaskSpec {
	return orchestrator.TaskSpec{
		TaskID:            id,
		Title:             id,
		Goal:              id,
		Targets:           []string{"127.0.0.1"},
		DependsOn:         deps,
		Priority:          80,
		Strategy:          "recon_seed",
		Action:            orchestrator.TaskAction{Type: "assist", Prompt: "probe"},
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"failed"},
		ExpectedArtifacts: []string{id + ".log"},
		RiskLevel:         string(orchestrator.RiskReconReadonly),
		Budget: orchestrator.TaskBudget{
			MaxSteps:     6,
			MaxToolCalls: 8,
			MaxRuntime:   2 * time.Minute,
		},
	}
}
