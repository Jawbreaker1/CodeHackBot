package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

const (
	defaultPlannerPlaybookMax    = 2
	defaultPlannerPlaybookLines  = 60
	defaultPlannerLLMMaxAttempts = 6
	defaultPlannerLLMMaxDuration = 90 * time.Second
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
		return fmt.Errorf("at least one --scope-target, --scope-network, or --scope-local is required with --goal")
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
	sessionsDir string,
	runID, goal string,
	scope orchestrator.Scope,
	constraints, successCriteria, stopCriteria []string,
	maxParallelism, hypothesisLimit int,
	now time.Time,
) (orchestrator.RunPlan, string, error) {
	if plannerMode == "llm" || plannerMode == "auto" {
		cfg, client, model, llmCfgErr := resolvePlannerLLMClient(workerConfigPath)
		if llmCfgErr != nil || client == nil || strings.TrimSpace(model) == "" {
			if llmCfgErr == nil {
				llmCfgErr = fmt.Errorf("llm client/model unavailable")
			}
			return orchestrator.RunPlan{}, "", fmt.Errorf("planner mode %q requires LLM availability; rerun with --planner static for explicit static planning: %w", plannerMode, llmCfgErr)
		}
		playbookHints, playbookNames := plannerPlaybookHints(goal, constraints, cfg)
		attempts := defaultPlannerLLMMaxAttempts
		if attempts < 1 {
			attempts = 1
		}
		maxDuration := defaultPlannerLLMMaxDuration
		if maxDuration <= 0 {
			maxDuration = 30 * time.Second
		}
		started := time.Now().UTC()
		fingerprintHits := map[string]int{}
		diagnostics := make([]plannerAttemptDiagnostic, 0, attempts)
		var lastErr error
		for attempt := 1; attempt <= attempts; attempt++ {
			if time.Since(started) > maxDuration {
				lastErr = fmt.Errorf("planner retry budget exceeded after %s", maxDuration)
				break
			}

			attemptHypothesisLimit := adaptivePlannerHypothesisLimit(hypothesisLimit, attempt)
			attemptPlaybookHints := playbookHints
			attemptPlaybookNames := playbookNames
			playbooksIncluded := strings.TrimSpace(attemptPlaybookHints) != ""
			if attempt >= 4 {
				attemptPlaybookHints = ""
				attemptPlaybookNames = nil
				playbooksIncluded = false
			}

			useJSONSchema := true
			attemptMaxTokens := adaptivePlannerMaxTokens(cfg, attempt)
			plan, llmRationale, err := buildGoalLLMPlanWithStructuredOutput(
				ctx,
				cfg,
				client,
				model,
				runID,
				goal,
				scope,
				constraints,
				successCriteria,
				stopCriteria,
				maxParallelism,
				attemptHypothesisLimit,
				attemptPlaybookHints,
				attemptPlaybookNames,
				now,
				useJSONSchema,
				attemptMaxTokens,
			)
			if err == nil {
				if attempt > 1 {
					_, _ = persistPlannerAttemptDiagnostics(sessionsDir, runID, diagnostics)
				}
				if attempt <= 1 {
					return plan, llmRationale, nil
				}
				retryNote := fmt.Sprintf("llm planner succeeded after retry %d/%d", attempt, attempts)
				if strings.TrimSpace(llmRationale) == "" {
					return plan, retryNote, nil
				}
				return plan, llmRationale + " | " + retryNote, nil
			}
			lastErr = err

			diag := plannerAttemptDiagnostic{
				Attempt:           attempt,
				WhenUTC:           time.Now().UTC(),
				Outcome:           "failed",
				Error:             err.Error(),
				HypothesisLimit:   attemptHypothesisLimit,
				PlaybooksIncluded: playbooksIncluded,
				JSONSchemaEnabled: useJSONSchema,
				MaxTokens:         attemptMaxTokens,
			}
			var plannerFailure *orchestrator.LLMPlannerFailure
			if errors.As(err, &plannerFailure) {
				diag.FailureStage = strings.TrimSpace(plannerFailure.Stage)
				diag.FinishReason = strings.TrimSpace(plannerFailure.FinishReason)
				diag.Fingerprint = strings.TrimSpace(plannerFailure.Fingerprint)
				diag.RawResponse = strings.TrimSpace(plannerFailure.RawResponse)
				diag.ExtractedJSON = strings.TrimSpace(plannerFailure.Extracted)
				if diag.Fingerprint != "" {
					fingerprintHits[diag.Fingerprint]++
					if fingerprintHits[diag.Fingerprint] >= 2 {
						diagnostics = append(diagnostics, diag)
						lastErr = fmt.Errorf("%w (repeated planner fingerprint=%s)", err, diag.Fingerprint)
						break
					}
				}
			}
			diagnostics = append(diagnostics, diag)

			if attempt < attempts && time.Since(started) < maxDuration {
				time.Sleep(adaptivePlannerBackoff(attempt))
			}
		}
		diagPath, diagErr := persistPlannerAttemptDiagnostics(sessionsDir, runID, diagnostics)
		if diagErr == nil && strings.TrimSpace(diagPath) != "" {
			return orchestrator.RunPlan{}, "", fmt.Errorf("llm planner failed after %d attempt(s) in mode %q; rerun with --planner static for explicit static planning: %w (planner_attempts=%s)", len(diagnostics), plannerMode, lastErr, diagPath)
		}
		return orchestrator.RunPlan{}, "", fmt.Errorf("llm planner failed after %d attempt(s) in mode %q; rerun with --planner static for explicit static planning: %w", len(diagnostics), plannerMode, lastErr)
	}
	plan, err := buildGoalSeedPlan(runID, goal, scope, constraints, successCriteria, stopCriteria, maxParallelism, hypothesisLimit, now)
	return plan, "", err
}

func buildGoalLLMPlan(
	ctx context.Context,
	cfg config.Config,
	client llm.Client,
	model string,
	runID, goal string,
	scope orchestrator.Scope,
	constraints, successCriteria, stopCriteria []string,
	maxParallelism, hypothesisLimit int,
	playbookHints string,
	playbookNames []string,
	now time.Time,
) (orchestrator.RunPlan, string, error) {
	return buildGoalLLMPlanWithStructuredOutput(
		ctx,
		cfg,
		client,
		model,
		runID,
		goal,
		scope,
		constraints,
		successCriteria,
		stopCriteria,
		maxParallelism,
		hypothesisLimit,
		playbookHints,
		playbookNames,
		now,
		true,
		0,
	)
}

func buildGoalLLMPlanWithStructuredOutput(
	ctx context.Context,
	cfg config.Config,
	client llm.Client,
	model string,
	runID, goal string,
	scope orchestrator.Scope,
	constraints, successCriteria, stopCriteria []string,
	maxParallelism, hypothesisLimit int,
	playbookHints string,
	playbookNames []string,
	now time.Time,
	useJSONSchema bool,
	maxTokensOverride int,
) (orchestrator.RunPlan, string, error) {
	normalizedGoal := normalizeGoal(goal)
	hypotheses := orchestrator.GenerateHypotheses(normalizedGoal, scope, hypothesisLimit)
	temperature, maxTokens := cfg.ResolveLLMRoleOptions("planner", 0.05, 1800)
	if maxTokensOverride > 0 {
		maxTokens = maxTokensOverride
	}
	useJSONSchemaPtr := useJSONSchema
	tasks, llmRationale, err := orchestrator.SynthesizeTaskGraphWithLLMWithOptions(
		ctx,
		client,
		model,
		normalizedGoal,
		scope,
		constraints,
		hypotheses,
		maxParallelism,
		orchestrator.LLMPlannerOptions{
			Temperature:   float32Ptr(temperature),
			MaxTokens:     intPtrPositive(maxTokens),
			Playbooks:     strings.TrimSpace(playbookHints),
			UseJSONSchema: &useJSONSchemaPtr,
		},
	)
	if err != nil {
		return orchestrator.RunPlan{}, "", err
	}
	if normalizedTasks, note := normalizeArchiveWorkflowTaskDependencies(normalizedGoal, tasks); note != "" {
		tasks = normalizedTasks
		llmRationale = mergePlannerRationale(llmRationale, note)
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
			CreatedAt:        now.UTC(),
			RunPhase:         orchestrator.RunPhaseReview,
			Goal:             strings.TrimSpace(goal),
			NormalizedGoal:   normalizedGoal,
			PlannerMode:      plannerModeLLMV1,
			PlannerModel:     strings.TrimSpace(model),
			PlannerPlaybooks: compactStrings(playbookNames),
			Hypotheses:       hypotheses,
		},
	}
	if err := orchestrator.ValidateSynthesizedPlan(plan); err != nil {
		return orchestrator.RunPlan{}, "", err
	}
	return plan, strings.TrimSpace(llmRationale), nil
}

func resolvePlannerLLMClient(workerConfigPath string) (config.Config, llm.Client, string, error) {
	cfg, err := loadPlannerLLMConfig(workerConfigPath)
	if err != nil {
		return config.Config{}, nil, "", err
	}
	if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
		return config.Config{}, nil, "", fmt.Errorf("llm planner requires %s or llm.base_url in config", plannerLLMBaseURLEnv)
	}
	model := strings.TrimSpace(cfg.LLM.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.Agent.Model)
	}
	if model == "" {
		return config.Config{}, nil, "", fmt.Errorf("llm planner requires %s or llm.model in config", plannerLLMModelEnv)
	}
	return cfg, llm.NewLMStudioClient(cfg), model, nil
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
	if normalizedTasks, _ := normalizeArchiveWorkflowTaskDependencies(normalizedGoal, tasks); len(normalizedTasks) > 0 {
		tasks = normalizedTasks
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
