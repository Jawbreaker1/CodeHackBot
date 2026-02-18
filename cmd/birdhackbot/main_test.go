package main

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestRunWorkerModeUsesOrchestratorEnv(t *testing.T) {
	base := t.TempDir()
	runID := "run-main-worker"
	taskID := "task-1"
	if _, err := orchestrator.EnsureRunLayout(base, runID); err != nil {
		t.Fatalf("EnsureRunLayout: %v", err)
	}
	planPath := filepath.Join(orchestrator.BuildRunPaths(base, runID).PlanDir, "plan.json")
	if err := orchestrator.WriteJSONAtomic(planPath, orchestrator.RunPlan{
		RunID:           runID,
		Scope:           orchestrator.Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []orchestrator.TaskSpec{},
	}); err != nil {
		t.Fatalf("WriteJSONAtomic plan: %v", err)
	}

	cmd, args := shellEchoCommand("worker-mode-ok")
	task := orchestrator.TaskSpec{
		TaskID:            taskID,
		Goal:              "worker mode test",
		DoneWhen:          []string{"done"},
		FailWhen:          []string{"failed"},
		ExpectedArtifacts: []string{"log"},
		RiskLevel:         string(orchestrator.RiskReconReadonly),
		Action: orchestrator.TaskAction{
			Type:    "command",
			Command: cmd,
			Args:    args,
		},
		Budget: orchestrator.TaskBudget{
			MaxSteps:     2,
			MaxToolCalls: 2,
			MaxRuntime:   10 * time.Second,
		},
	}
	taskPath := filepath.Join(orchestrator.BuildRunPaths(base, runID).TaskDir, taskID+".json")
	if err := orchestrator.WriteJSONAtomic(taskPath, task); err != nil {
		t.Fatalf("WriteJSONAtomic task: %v", err)
	}

	t.Setenv(orchestrator.OrchSessionsDirEnv, base)
	t.Setenv(orchestrator.OrchRunIDEnv, runID)
	t.Setenv(orchestrator.OrchTaskIDEnv, taskID)
	t.Setenv(orchestrator.OrchWorkerIDEnv, "worker-task-1")
	t.Setenv(orchestrator.OrchAttemptEnv, "1")

	if code := runWorkerMode(nil); code != 0 {
		t.Fatalf("runWorkerMode failed with code %d", code)
	}
}

func shellEchoCommand(message string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", "echo " + message}
	}
	return "sh", []string{"-c", "printf '%s\\n' " + message}
}
