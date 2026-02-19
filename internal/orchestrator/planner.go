package orchestrator

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	maxSynthesizedTasks         = 20
	maxSynthesizedTaskRuntime   = 15 * time.Minute
	maxSynthesizedTaskSteps     = 300
	maxSynthesizedTaskToolCalls = 300
)

func SynthesizeTaskGraph(goal string, scope Scope, hypotheses []Hypothesis) ([]TaskSpec, error) {
	normalizedGoal := strings.TrimSpace(goal)
	if normalizedGoal == "" {
		return nil, fmt.Errorf("%w: goal is required for task synthesis", ErrInvalidPlan)
	}
	if len(hypotheses) == 0 {
		return nil, fmt.Errorf("%w: at least one hypothesis is required for task synthesis", ErrInvalidPlan)
	}

	targets := scopeTargets(scope)
	rootTaskID := "task-recon-seed"
	tasks := []TaskSpec{
		{
			TaskID:            rootTaskID,
			Title:             "Seed reconnaissance",
			Goal:              "Establish baseline evidence for goal: " + normalizedGoal,
			Targets:           targets,
			Priority:          100,
			Strategy:          "recon_seed",
			Action:            assistAction("Establish baseline reconnaissance evidence for: " + normalizedGoal),
			DoneWhen:          []string{"baseline_scope_inventory_captured"},
			FailWhen:          []string{"scope_inventory_failed", "seed_timeout"},
			ExpectedArtifacts: []string{"recon-seed.log"},
			RiskLevel:         string(RiskReconReadonly),
			Budget: TaskBudget{
				MaxSteps:     2,
				MaxToolCalls: 2,
				MaxRuntime:   45 * time.Second,
			},
		},
	}

	hypothesisTaskIDs := make([]string, 0, len(hypotheses))
	for idx, hypothesis := range hypotheses {
		taskID := fmt.Sprintf("task-h%02d", idx+1)
		riskLevel := riskLevelFromHypothesis(hypothesis)
		budget := budgetForRiskLevel(riskLevel)
		doneWhen := append([]string{}, hypothesis.SuccessSignals...)
		if len(doneWhen) == 0 {
			doneWhen = []string{"hypothesis_supported"}
		}
		failWhen := append([]string{}, hypothesis.FailSignals...)
		if len(failWhen) == 0 {
			failWhen = []string{"hypothesis_not_supported", "hypothesis_timeout"}
		}
		artifactName := fmt.Sprintf("hypothesis-%s.log", strings.ToLower(strings.ReplaceAll(hypothesis.ID, " ", "-")))
		priority := 90 - idx
		if priority < 1 {
			priority = 1
		}
		tasks = append(tasks, TaskSpec{
			TaskID:            taskID,
			Title:             fmt.Sprintf("Validate %s", hypothesis.ID),
			Goal:              hypothesis.Statement,
			Targets:           targets,
			DependsOn:         []string{rootTaskID},
			Priority:          priority,
			Strategy:          "hypothesis_validate",
			Action:            assistAction("Validate hypothesis " + hypothesis.ID + ": " + hypothesis.Statement),
			DoneWhen:          doneWhen,
			FailWhen:          failWhen,
			ExpectedArtifacts: []string{artifactName},
			RiskLevel:         riskLevel,
			Budget:            budget,
		})
		hypothesisTaskIDs = append(hypothesisTaskIDs, taskID)
	}

	summaryDepends := append([]string{rootTaskID}, hypothesisTaskIDs...)
	tasks = append(tasks, TaskSpec{
		TaskID:            "task-plan-summary",
		Title:             "Plan synthesis summary",
		Goal:              "Consolidate hypothesis outcomes for next planning iteration",
		Targets:           targets,
		DependsOn:         summaryDepends,
		Priority:          10,
		Strategy:          "summarize_and_replan",
		Action:            assistAction("Consolidate hypothesis outcomes, summarize findings, and propose next steps."),
		DoneWhen:          []string{"hypothesis_summary_recorded"},
		FailWhen:          []string{"summary_failed", "summary_timeout"},
		ExpectedArtifacts: []string{"plan-summary.log"},
		RiskLevel:         string(RiskReconReadonly),
		Budget: TaskBudget{
			MaxSteps:     3,
			MaxToolCalls: 3,
			MaxRuntime:   90 * time.Second,
		},
	})

	return tasks, nil
}

func ValidateSynthesizedPlan(plan RunPlan) error {
	if err := ValidatePlanForStart(plan); err != nil {
		return err
	}
	if len(plan.Tasks) > maxSynthesizedTasks {
		return fmt.Errorf("%w: synthesized task count %d exceeds max %d", ErrInvalidPlan, len(plan.Tasks), maxSynthesizedTasks)
	}
	policy := NewScopePolicy(plan.Scope)
	for _, task := range plan.Tasks {
		if err := policy.ValidateTaskTargets(task); err != nil {
			return fmt.Errorf("%w: task %s target validation failed: %v", ErrInvalidPlan, task.TaskID, err)
		}
		riskTier, err := ParseRiskTier(task.RiskLevel)
		if err != nil {
			return fmt.Errorf("%w: task %s invalid risk level: %v", ErrInvalidPlan, task.TaskID, err)
		}
		// Synthesized planning is intentionally bounded to recon/active probe for safe default autonomy.
		if riskTier != RiskReconReadonly && riskTier != RiskActiveProbe {
			return fmt.Errorf("%w: task %s risk level %s is not allowed for synthesized plans", ErrInvalidPlan, task.TaskID, task.RiskLevel)
		}
		if task.Budget.MaxRuntime > maxSynthesizedTaskRuntime {
			return fmt.Errorf("%w: task %s runtime budget exceeds %s", ErrInvalidPlan, task.TaskID, maxSynthesizedTaskRuntime)
		}
		if task.Budget.MaxSteps > maxSynthesizedTaskSteps || task.Budget.MaxToolCalls > maxSynthesizedTaskToolCalls {
			return fmt.Errorf("%w: task %s budget exceeds synthesized limits", ErrInvalidPlan, task.TaskID)
		}
	}
	if _, err := NewScheduler(plan, plan.MaxParallelism); err != nil {
		return fmt.Errorf("%w: scheduler preflight failed: %v", ErrInvalidPlan, err)
	}
	return nil
}

func scopeTargets(scope Scope) []string {
	combined := append([]string{}, scope.Targets...)
	combined = append(combined, scope.Networks...)
	out := make([]string, 0, len(combined))
	seen := make(map[string]struct{}, len(combined))
	for _, value := range combined {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func riskLevelFromHypothesis(hypothesis Hypothesis) string {
	impact := strings.ToLower(strings.TrimSpace(hypothesis.Impact))
	confidence := strings.ToLower(strings.TrimSpace(hypothesis.Confidence))
	if impact == "high" && confidence != "low" {
		return string(RiskActiveProbe)
	}
	return string(RiskReconReadonly)
}

func budgetForRiskLevel(riskLevel string) TaskBudget {
	if riskLevel == string(RiskActiveProbe) {
		return TaskBudget{
			MaxSteps:     10,
			MaxToolCalls: 10,
			MaxRuntime:   4 * time.Minute,
		}
	}
	return TaskBudget{
		MaxSteps:     6,
		MaxToolCalls: 6,
		MaxRuntime:   2 * time.Minute,
	}
}

func echoAction(message string) TaskAction {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		trimmed = "task"
	}
	if runtime.GOOS == "windows" {
		return TaskAction{
			Type:    "command",
			Command: "cmd",
			Args:    []string{"/C", "echo " + trimmed},
		}
	}
	return TaskAction{
		Type:    "command",
		Command: "printf",
		Args:    []string{"%s\n", trimmed},
	}
}

func assistAction(prompt string) TaskAction {
	return TaskAction{
		Type:   "assist",
		Prompt: strings.TrimSpace(prompt),
	}
}

func hasDependency(task TaskSpec, dep string) bool {
	return slices.Contains(task.DependsOn, dep)
}
