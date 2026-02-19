package orchestrator

import (
	"fmt"
	"net"
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
	reconTaskIDs := make([]string, 0, 4)
	tasks := make([]TaskSpec, 0, 8)
	fanoutRanges := splitScopeNetworksForFanout(scope.Networks, 4)
	if len(fanoutRanges) > 0 {
		for i, subnet := range fanoutRanges {
			taskID := fmt.Sprintf("task-recon-range-%02d", i+1)
			reconTaskIDs = append(reconTaskIDs, taskID)
			tasks = append(tasks, TaskSpec{
				TaskID:            taskID,
				Title:             fmt.Sprintf("Reconnaissance subnet %s", subnet),
				Goal:              fmt.Sprintf("Enumerate hosts and services in subnet %s for goal: %s", subnet, normalizedGoal),
				Targets:           []string{subnet},
				Priority:          100 - i,
				Strategy:          "recon_fanout",
				Action:            assistAction(fmt.Sprintf("Enumerate hosts and services in subnet %s. Prefer target-aware, non-redundant recon steps.", subnet)),
				DoneWhen:          []string{"subnet_recon_completed"},
				FailWhen:          []string{"subnet_recon_failed", "subnet_recon_timeout"},
				ExpectedArtifacts: []string{fmt.Sprintf("recon-%s.log", strings.ReplaceAll(subnet, "/", "_"))},
				RiskLevel:         string(RiskReconReadonly),
				Budget: TaskBudget{
					MaxSteps:     20,
					MaxToolCalls: 30,
					MaxRuntime:   10 * time.Minute,
				},
			})
		}
	} else {
		rootTaskID := "task-recon-seed"
		reconTaskIDs = append(reconTaskIDs, rootTaskID)
		tasks = append(tasks, TaskSpec{
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
				MaxSteps:     20,
				MaxToolCalls: 30,
				MaxRuntime:   10 * time.Minute,
			},
		})
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
			DependsOn:         append([]string{}, reconTaskIDs...),
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

	summaryDepends := append([]string{}, reconTaskIDs...)
	summaryDepends = append(summaryDepends, hypothesisTaskIDs...)
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
			MaxSteps:     6,
			MaxToolCalls: 8,
			MaxRuntime:   3 * time.Minute,
		},
	})

	return tasks, nil
}

func splitScopeNetworksForFanout(networks []string, maxRanges int) []string {
	if maxRanges <= 0 || len(networks) == 0 {
		return nil
	}
	out := make([]string, 0, maxRanges)
	seen := map[string]struct{}{}
	for _, raw := range networks {
		if len(out) >= maxRanges {
			break
		}
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		_, parsed, err := net.ParseCIDR(token)
		if err != nil || parsed == nil {
			continue
		}
		subnets := splitCIDR(parsed, 4)
		for _, subnet := range subnets {
			if len(out) >= maxRanges {
				break
			}
			value := subnet.String()
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func splitCIDR(network *net.IPNet, parts int) []*net.IPNet {
	if network == nil || parts <= 1 {
		return []*net.IPNet{network}
	}
	ip := network.IP.To4()
	if ip == nil {
		return []*net.IPNet{network}
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return []*net.IPNet{network}
	}
	// Split into exactly 4 subranges when possible by increasing prefix by 2.
	newOnes := ones + 2
	if newOnes > 32 {
		return []*net.IPNet{network}
	}
	base := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	step := uint32(1) << uint32(32-newOnes)
	out := make([]*net.IPNet, 0, 4)
	for i := 0; i < 4; i++ {
		next := base + (uint32(i) * step)
		nextIP := net.IPv4(byte(next>>24), byte(next>>16), byte(next>>8), byte(next))
		out = append(out, &net.IPNet{IP: nextIP.Mask(net.CIDRMask(newOnes, 32)), Mask: net.CIDRMask(newOnes, 32)})
	}
	return out
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
			MaxSteps:     24,
			MaxToolCalls: 40,
			MaxRuntime:   12 * time.Minute,
		}
	}
	return TaskBudget{
		MaxSteps:     16,
		MaxToolCalls: 24,
		MaxRuntime:   8 * time.Minute,
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
