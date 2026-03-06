package main

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

type plannerDependencyRepairStats struct {
	RemovedUnknown   int
	RemovedSelf      int
	RemovedForward   int
	RemovedDuplicate int
}

func (s plannerDependencyRepairStats) changed() bool {
	return s.RemovedUnknown+s.RemovedSelf+s.RemovedForward+s.RemovedDuplicate > 0
}

func (s plannerDependencyRepairStats) note() string {
	parts := make([]string, 0, 4)
	if s.RemovedForward > 0 {
		parts = append(parts, fmt.Sprintf("forward=%d", s.RemovedForward))
	}
	if s.RemovedUnknown > 0 {
		parts = append(parts, fmt.Sprintf("unknown=%d", s.RemovedUnknown))
	}
	if s.RemovedSelf > 0 {
		parts = append(parts, fmt.Sprintf("self=%d", s.RemovedSelf))
	}
	if s.RemovedDuplicate > 0 {
		parts = append(parts, fmt.Sprintf("duplicate=%d", s.RemovedDuplicate))
	}
	return strings.Join(parts, ",")
}

func maybeRepairPlannerPreflightFailure(plan orchestrator.RunPlan, validationErr error) (orchestrator.RunPlan, string, bool) {
	if !isRepairablePlannerPreflightError(validationErr) {
		return orchestrator.RunPlan{}, "", false
	}
	repaired, stats := repairPlannerDependencies(plan)
	if !stats.changed() {
		return orchestrator.RunPlan{}, "", false
	}
	if err := orchestrator.ValidateSynthesizedPlan(repaired); err != nil {
		return orchestrator.RunPlan{}, "", false
	}
	note := "planner dependency graph auto-repaired"
	if details := strings.TrimSpace(stats.note()); details != "" {
		note += " (" + details + ")"
	}
	return repaired, note, true
}

func isRepairablePlannerPreflightError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" || !strings.Contains(msg, "scheduler preflight failed:") {
		return false
	}
	return strings.Contains(msg, "cycle detected at task") ||
		strings.Contains(msg, "depends on unknown task")
}

func repairPlannerDependencies(plan orchestrator.RunPlan) (orchestrator.RunPlan, plannerDependencyRepairStats) {
	repaired := plan
	if len(plan.Tasks) == 0 {
		return repaired, plannerDependencyRepairStats{}
	}

	indexByID := make(map[string]int, len(plan.Tasks))
	for idx, task := range plan.Tasks {
		indexByID[task.TaskID] = idx
	}

	stats := plannerDependencyRepairStats{}
	repaired.Tasks = append([]orchestrator.TaskSpec(nil), plan.Tasks...)
	for idx := range repaired.Tasks {
		task := repaired.Tasks[idx]
		filtered := make([]string, 0, len(task.DependsOn))
		seen := make(map[string]struct{}, len(task.DependsOn))
		for _, rawDep := range task.DependsOn {
			dep := strings.TrimSpace(rawDep)
			if dep == "" {
				continue
			}
			if _, exists := seen[dep]; exists {
				stats.RemovedDuplicate++
				continue
			}
			seen[dep] = struct{}{}
			if dep == task.TaskID {
				stats.RemovedSelf++
				continue
			}
			depIdx, exists := indexByID[dep]
			if !exists {
				stats.RemovedUnknown++
				continue
			}
			if depIdx >= idx {
				stats.RemovedForward++
				continue
			}
			filtered = append(filtered, dep)
		}
		task.DependsOn = filtered
		repaired.Tasks[idx] = task
	}
	return repaired, stats
}
