package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func plannerPromptHash(goal, plannerMode string, scope orchestrator.Scope, constraints, successCriteria, stopCriteria []string, maxParallelism int, playbooks []string) string {
	payload := map[string]any{
		"version":           plannerVersion,
		"planner_mode":      strings.TrimSpace(plannerMode),
		"goal":              normalizeGoal(goal),
		"scope":             scope,
		"constraints":       constraints,
		"success_criteria":  successCriteria,
		"stop_criteria":     stopCriteria,
		"max_parallelism":   maxParallelism,
		"planner_playbooks": compactStrings(playbooks),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func printPlanSummary(stdout io.Writer, plan orchestrator.RunPlan) {
	fmt.Fprintf(stdout, "plan summary: run=%s planner=%s mode=%s prompt_hash=%s hypotheses=%d tasks=%d max_parallelism=%d\n", plan.RunID, plan.Metadata.PlannerVersion, plan.Metadata.PlannerMode, plan.Metadata.PlannerPromptHash, len(plan.Metadata.Hypotheses), len(plan.Tasks), plan.MaxParallelism)
	for i, hypothesis := range plan.Metadata.Hypotheses {
		if i >= 3 {
			fmt.Fprintf(stdout, "  ... %d more hypotheses\n", len(plan.Metadata.Hypotheses)-i)
			break
		}
		fmt.Fprintf(stdout, "  [%s] impact=%s confidence=%s score=%d: %s\n", hypothesis.ID, hypothesis.Impact, hypothesis.Confidence, hypothesis.Score, hypothesis.Statement)
	}
	for i, task := range plan.Tasks {
		if i >= 5 {
			fmt.Fprintf(stdout, "  ... %d more tasks\n", len(plan.Tasks)-i)
			break
		}
		fmt.Fprintf(stdout, "  task[%d] %s risk=%s depends_on=%d strategy=%s\n", i+1, task.TaskID, task.RiskLevel, len(task.DependsOn), task.Strategy)
	}
}

func float32Ptr(v float32) *float32 {
	return &v
}

func intPtrPositive(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func persistPlanReview(sessionsDir string, plan orchestrator.RunPlan, planFilename string) (string, string, error) {
	paths, err := orchestrator.EnsureRunLayout(sessionsDir, plan.RunID)
	if err != nil {
		return "", "", err
	}
	planPath := filepath.Join(paths.PlanDir, planFilename)
	if err := orchestrator.WriteJSONAtomic(planPath, plan); err != nil {
		return "", "", err
	}
	reviewPath := filepath.Join(paths.PlanDir, "plan.review.audit.json")
	review := map[string]any{
		"run_id":              plan.RunID,
		"run_phase":           plan.Metadata.RunPhase,
		"planner_mode":        plan.Metadata.PlannerMode,
		"planner_version":     plan.Metadata.PlannerVersion,
		"planner_model":       plan.Metadata.PlannerModel,
		"planner_playbooks":   plan.Metadata.PlannerPlaybooks,
		"planner_prompt_hash": plan.Metadata.PlannerPromptHash,
		"decision":            plan.Metadata.PlannerDecision,
		"rationale":           plan.Metadata.PlannerRationale,
		"regeneration_count":  plan.Metadata.RegenerationCount,
		"created_at":          plan.Metadata.CreatedAt,
		"goal":                plan.Metadata.Goal,
		"normalized_goal":     plan.Metadata.NormalizedGoal,
		"hypothesis_count":    len(plan.Metadata.Hypotheses),
		"task_count":          len(plan.Tasks),
	}
	if err := orchestrator.WriteJSONAtomic(reviewPath, review); err != nil {
		return "", "", err
	}
	return planPath, reviewPath, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
