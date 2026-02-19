package orchestrator

import (
	"testing"
	"time"
)

func TestSynthesizeTaskGraphBuildsDependencyAwareGraph(t *testing.T) {
	t.Parallel()

	hypotheses := []Hypothesis{
		{
			ID:               "H-01",
			Statement:        "Network services may expose vulnerable versions.",
			Confidence:       "high",
			Impact:           "high",
			SuccessSignals:   []string{"services fingerprinted"},
			FailSignals:      []string{"host unreachable"},
			EvidenceRequired: []string{"nmap output"},
		},
		{
			ID:               "H-02",
			Statement:        "Web surface may expose weak input handling.",
			Confidence:       "medium",
			Impact:           "medium",
			SuccessSignals:   []string{"input vectors identified"},
			FailSignals:      []string{"no web surface"},
			EvidenceRequired: []string{"endpoint map"},
		},
	}
	scope := Scope{Targets: []string{"192.168.50.10"}}
	tasks, err := SynthesizeTaskGraph("assess target", scope, hypotheses)
	if err != nil {
		t.Fatalf("SynthesizeTaskGraph: %v", err)
	}
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks (recon + 2 hypothesis + summary), got %d", len(tasks))
	}
	if tasks[0].TaskID != "task-recon-seed" {
		t.Fatalf("unexpected root task id: %s", tasks[0].TaskID)
	}
	if tasks[1].DependsOn[0] != "task-recon-seed" || tasks[2].DependsOn[0] != "task-recon-seed" {
		t.Fatalf("hypothesis tasks should depend on recon seed")
	}
	summary := tasks[3]
	if summary.TaskID != "task-plan-summary" {
		t.Fatalf("unexpected summary task id: %s", summary.TaskID)
	}
	if len(summary.DependsOn) != 3 {
		t.Fatalf("expected summary task to depend on recon + hypotheses, got %d deps", len(summary.DependsOn))
	}
	for _, task := range tasks {
		if len(task.DoneWhen) == 0 || len(task.FailWhen) == 0 || len(task.ExpectedArtifacts) == 0 {
			t.Fatalf("task missing contract fields: %#v", task)
		}
		if task.Budget.MaxRuntime <= 0 || task.Budget.MaxSteps <= 0 || task.Budget.MaxToolCalls <= 0 {
			t.Fatalf("task missing bounded budget: %#v", task)
		}
	}
}

func TestValidateSynthesizedPlanRejectsUnsafeRisk(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "run-unsafe",
		Scope:           Scope{Targets: []string{"127.0.0.1"}},
		Constraints:     []string{"local_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "unsafe",
				Targets:           []string{"127.0.0.1"},
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"failed"},
				ExpectedArtifacts: []string{"out.log"},
				RiskLevel:         string(RiskDisruptive),
				Budget: TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Second,
				},
				Action: echoAction("unsafe"),
			},
		},
	}
	if err := ValidateSynthesizedPlan(plan); err == nil {
		t.Fatalf("expected synthesized preflight rejection for disruptive risk")
	}
}

func TestValidateSynthesizedPlanRejectsOutOfScopeTargets(t *testing.T) {
	t.Parallel()

	plan := RunPlan{
		RunID:           "run-scope-violation",
		Scope:           Scope{Targets: []string{"192.168.50.10"}},
		Constraints:     []string{"internal_only"},
		SuccessCriteria: []string{"done"},
		StopCriteria:    []string{"stop"},
		MaxParallelism:  1,
		Tasks: []TaskSpec{
			{
				TaskID:            "t1",
				Goal:              "scope test",
				Targets:           []string{"10.10.10.10"},
				DoneWhen:          []string{"done"},
				FailWhen:          []string{"failed"},
				ExpectedArtifacts: []string{"out.log"},
				RiskLevel:         string(RiskReconReadonly),
				Budget: TaskBudget{
					MaxSteps:     1,
					MaxToolCalls: 1,
					MaxRuntime:   time.Second,
				},
				Action: echoAction("scope"),
			},
		},
	}
	if err := ValidateSynthesizedPlan(plan); err == nil {
		t.Fatalf("expected synthesized preflight rejection for out-of-scope target")
	}
}
