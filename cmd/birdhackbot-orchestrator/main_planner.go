package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func normalizeGoal(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}

func compactStringFlags(values stringFlags) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
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

func validateGoalSeedInputs(scope orchestrator.Scope, constraints []string) error {
	if len(scope.Targets) == 0 && len(scope.Networks) == 0 {
		return fmt.Errorf("at least one --scope-target or --scope-network is required with --goal")
	}
	if len(constraints) == 0 {
		return fmt.Errorf("at least one --constraint is required with --goal")
	}
	return nil
}

func ensureRunIDAvailable(sessionsDir, runID string) error {
	runRoot := orchestrator.BuildRunPaths(sessionsDir, runID).Root
	_, err := os.Stat(runRoot)
	if err == nil {
		return fmt.Errorf("run id %q already exists", runID)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("check run path: %w", err)
	}
	return nil
}

func generateRunID(now time.Time) string {
	utc := now.UTC()
	return fmt.Sprintf("run-%s-%04x", utc.Format("20060102-150405"), utc.UnixNano()&0xffff)
}

func buildGoalPlanFromMode(
	ctx context.Context,
	plannerMode string,
	workerConfigPath string,
	runID, goal string,
	scope orchestrator.Scope,
	constraints, successCriteria, stopCriteria []string,
	maxParallelism, hypothesisLimit int,
	now time.Time,
) (orchestrator.RunPlan, string, error) {
	if plannerMode == "llm" || plannerMode == "auto" {
		client, model, llmCfgErr := resolvePlannerLLMClient(workerConfigPath)
		if llmCfgErr == nil && client != nil && strings.TrimSpace(model) != "" {
			plan, llmRationale, err := buildGoalLLMPlan(ctx, client, model, runID, goal, scope, constraints, successCriteria, stopCriteria, maxParallelism, hypothesisLimit, now)
			if err == nil {
				return plan, llmRationale, nil
			}
			if plannerMode == "llm" {
				return orchestrator.RunPlan{}, "", err
			}
			fallback, fallbackErr := buildGoalSeedPlan(runID, goal, scope, constraints, successCriteria, stopCriteria, maxParallelism, hypothesisLimit, now)
			if fallbackErr != nil {
				return orchestrator.RunPlan{}, "", fmt.Errorf("llm planner failed (%v); static fallback failed: %w", err, fallbackErr)
			}
			note := fmt.Sprintf("auto fallback to static planner: %v", err)
			return fallback, note, nil
		}
		if plannerMode == "llm" {
			return orchestrator.RunPlan{}, "", llmCfgErr
		}
		fallback, fallbackErr := buildGoalSeedPlan(runID, goal, scope, constraints, successCriteria, stopCriteria, maxParallelism, hypothesisLimit, now)
		if fallbackErr != nil {
			return orchestrator.RunPlan{}, "", fallbackErr
		}
		note := ""
		if llmCfgErr != nil {
			note = fmt.Sprintf("auto fallback to static planner: %v", llmCfgErr)
		}
		return fallback, note, nil
	}
	plan, err := buildGoalSeedPlan(runID, goal, scope, constraints, successCriteria, stopCriteria, maxParallelism, hypothesisLimit, now)
	return plan, "", err
}

func buildGoalLLMPlan(
	ctx context.Context,
	client llm.Client,
	model string,
	runID, goal string,
	scope orchestrator.Scope,
	constraints, successCriteria, stopCriteria []string,
	maxParallelism, hypothesisLimit int,
	now time.Time,
) (orchestrator.RunPlan, string, error) {
	normalizedGoal := normalizeGoal(goal)
	hypotheses := orchestrator.GenerateHypotheses(normalizedGoal, scope, hypothesisLimit)
	tasks, llmRationale, err := orchestrator.SynthesizeTaskGraphWithLLM(ctx, client, model, normalizedGoal, scope, constraints, hypotheses, maxParallelism)
	if err != nil {
		return orchestrator.RunPlan{}, "", err
	}
	if maxParallelism <= 0 {
		maxParallelism = 1
	}
	if len(successCriteria) == 0 {
		successCriteria = []string{"goal_seed_completed"}
	}
	if len(stopCriteria) == 0 {
		stopCriteria = []string{"manual_stop", "out_of_scope", "budget_exhausted"}
	}
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           scope,
		Constraints:     constraints,
		SuccessCriteria: successCriteria,
		StopCriteria:    stopCriteria,
		MaxParallelism:  maxParallelism,
		Tasks:           tasks,
		Metadata: orchestrator.PlanMetadata{
			CreatedAt:      now.UTC(),
			RunPhase:       orchestrator.RunPhaseReview,
			Goal:           strings.TrimSpace(goal),
			NormalizedGoal: normalizedGoal,
			PlannerMode:    plannerModeLLMV1,
			Hypotheses:     hypotheses,
		},
	}
	if err := orchestrator.ValidateSynthesizedPlan(plan); err != nil {
		return orchestrator.RunPlan{}, "", err
	}
	return plan, strings.TrimSpace(llmRationale), nil
}

func resolvePlannerLLMClient(workerConfigPath string) (llm.Client, string, error) {
	cfg, err := loadPlannerLLMConfig(workerConfigPath)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
		return nil, "", fmt.Errorf("llm planner requires %s or llm.base_url in config", plannerLLMBaseURLEnv)
	}
	model := strings.TrimSpace(cfg.LLM.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.Agent.Model)
	}
	if model == "" {
		return nil, "", fmt.Errorf("llm planner requires %s or llm.model in config", plannerLLMModelEnv)
	}
	return llm.NewLMStudioClient(cfg), model, nil
}

func loadPlannerLLMConfig(workerConfigPath string) (config.Config, error) {
	cfg := config.Config{}
	cfg.LLM.TimeoutSeconds = plannerDefaultLLMTimeout
	if strings.TrimSpace(workerConfigPath) != "" {
		loaded, _, err := config.Load(workerConfigPath, "", "")
		if err != nil {
			return config.Config{}, fmt.Errorf("load config from %s: %w", workerConfigPath, err)
		}
		cfg = loaded
		if cfg.LLM.TimeoutSeconds <= 0 {
			cfg.LLM.TimeoutSeconds = plannerDefaultLLMTimeout
		}
	}

	if v := strings.TrimSpace(os.Getenv(plannerLLMBaseURLEnv)); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMModelEnv)); v != "" {
		cfg.LLM.Model = v
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMAPIKeyEnv)); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv(plannerLLMTimeoutEnv)); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return config.Config{}, fmt.Errorf("invalid %s value %q", plannerLLMTimeoutEnv, v)
		}
		cfg.LLM.TimeoutSeconds = parsed
	}
	return cfg, nil
}

func buildGoalSeedPlan(runID, goal string, scope orchestrator.Scope, constraints, successCriteria, stopCriteria []string, maxParallelism, hypothesisLimit int, now time.Time) (orchestrator.RunPlan, error) {
	normalizedGoal := normalizeGoal(goal)
	hypotheses := orchestrator.GenerateHypotheses(normalizedGoal, scope, hypothesisLimit)
	tasks, err := orchestrator.SynthesizeTaskGraph(normalizedGoal, scope, hypotheses)
	if err != nil {
		return orchestrator.RunPlan{}, err
	}
	if maxParallelism <= 0 {
		maxParallelism = 1
	}
	if len(successCriteria) == 0 {
		successCriteria = []string{"goal_seed_completed"}
	}
	if len(stopCriteria) == 0 {
		stopCriteria = []string{"manual_stop", "out_of_scope", "budget_exhausted"}
	}
	plan := orchestrator.RunPlan{
		RunID:           runID,
		Scope:           scope,
		Constraints:     constraints,
		SuccessCriteria: successCriteria,
		StopCriteria:    stopCriteria,
		MaxParallelism:  maxParallelism,
		Tasks:           tasks,
		Metadata: orchestrator.PlanMetadata{
			CreatedAt:      now.UTC(),
			RunPhase:       orchestrator.RunPhaseReview,
			Goal:           strings.TrimSpace(goal),
			NormalizedGoal: normalizedGoal,
			PlannerMode:    plannerModeStaticV1,
			Hypotheses:     hypotheses,
		},
	}
	if err := orchestrator.ValidateSynthesizedPlan(plan); err != nil {
		return orchestrator.RunPlan{}, err
	}
	return plan, nil
}

func buildInteractivePlanningPlan(runID string, now time.Time, scope orchestrator.Scope, constraints []string, maxParallelism int) orchestrator.RunPlan {
	if maxParallelism <= 0 {
		maxParallelism = 1
	}
	return orchestrator.RunPlan{
		RunID:           runID,
		Scope:           scope,
		Constraints:     constraints,
		SuccessCriteria: []string{},
		StopCriteria:    []string{},
		MaxParallelism:  maxParallelism,
		Tasks:           []orchestrator.TaskSpec{},
		Metadata: orchestrator.PlanMetadata{
			CreatedAt:      now.UTC(),
			RunPhase:       orchestrator.RunPhasePlanning,
			PlannerMode:    plannerModeTUIV1,
			PlannerVersion: plannerVersion,
		},
	}
}

func normalizePlanReview(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "approve":
		return "approve", nil
	case "reject", "edit", "regenerate":
		return strings.ToLower(strings.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("must be one of: approve|reject|edit|regenerate")
	}
}

func mergePlannerRationale(reviewRationale, plannerNote string) string {
	parts := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(reviewRationale); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := strings.TrimSpace(plannerNote); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, " | ")
}

func plannerPromptHash(goal, plannerMode string, scope orchestrator.Scope, constraints, successCriteria, stopCriteria []string, maxParallelism int) string {
	payload := map[string]any{
		"version":          plannerVersion,
		"planner_mode":     strings.TrimSpace(plannerMode),
		"goal":             normalizeGoal(goal),
		"scope":            scope,
		"constraints":      constraints,
		"success_criteria": successCriteria,
		"stop_criteria":    stopCriteria,
		"max_parallelism":  maxParallelism,
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
		"planner_version":     plan.Metadata.PlannerVersion,
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

func detectWorkerConfigPath() string {
	if existing := strings.TrimSpace(os.Getenv("BIRDHACKBOT_CONFIG_PATH")); existing != "" {
		if _, err := os.Stat(existing); err == nil {
			return existing
		}
	}
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return ""
	}
	candidate := filepath.Join(wd, "config", "default.json")
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
}
