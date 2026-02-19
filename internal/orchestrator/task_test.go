package orchestrator

import (
	"testing"
	"time"
)

func TestManagerAddTaskPersistsPlanAndTask(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-task-add"
	manager := NewManager(base)
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks:           []TaskSpec{task("t1", nil, 1)},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}

	added := task("t2", []string{"t1"}, 2)
	if err := manager.AddTask(runID, added); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	stored, err := manager.ReadTask(runID, "t2")
	if err != nil {
		t.Fatalf("ReadTask t2: %v", err)
	}
	if stored.TaskID != "t2" {
		t.Fatalf("unexpected stored task id: %s", stored.TaskID)
	}
	updatedPlan, err := manager.LoadRunPlan(runID)
	if err != nil {
		t.Fatalf("LoadRunPlan: %v", err)
	}
	if len(updatedPlan.Tasks) != 2 {
		t.Fatalf("expected two tasks after AddTask, got %d", len(updatedPlan.Tasks))
	}
}

func TestManagerAddTaskRejectsDuplicateTaskID(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	runID := "run-task-dup"
	manager := NewManager(base)
	plan := RunPlan{
		RunID:           runID,
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "baseline",
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"fail"},
				ExpectedArtifacts: []string{"artifact"},
				RiskLevel:         string(RiskReconReadonly),
				Budget: TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Second,
				},
			},
		},
	}
	if _, err := manager.StartFromPlan(plan, ""); err != nil {
		t.Fatalf("StartFromPlan: %v", err)
	}
	if err := manager.AddTask(runID, plan.Tasks[0]); err == nil {
		t.Fatalf("expected duplicate task id rejection")
	}
}
