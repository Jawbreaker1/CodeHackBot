package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"github.com/Jawbreaker1/CodeHackBot/internal/playbook"
)

const (
	defaultPlannerPlaybookMax    = 2
	defaultPlannerPlaybookLines  = 60
	defaultPlannerLLMMaxAttempts = 6
	defaultPlannerLLMMaxDuration = 90 * time.Second
)

type plannerAttemptDiagnostic struct {
	Attempt           int       `json:"attempt"`
	WhenUTC           time.Time `json:"when_utc"`
	Outcome           string    `json:"outcome"`
	Error             string    `json:"error,omitempty"`
	FailureStage      string    `json:"failure_stage,omitempty"`
	FinishReason      string    `json:"finish_reason,omitempty"`
	Fingerprint       string    `json:"fingerprint,omitempty"`
	HypothesisLimit   int       `json:"hypothesis_limit"`
	PlaybooksIncluded bool      `json:"playbooks_included"`
	JSONSchemaEnabled bool      `json:"json_schema_enabled"`
	MaxTokens         int       `json:"max_tokens"`
	RawResponse       string    `json:"-"`
	ExtractedJSON     string    `json:"-"`
}

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

func adaptivePlannerHypothesisLimit(baseLimit, attempt int) int {
	limit := baseLimit
	if limit <= 0 {
		limit = 5
	}
	if attempt <= 1 {
		return limit
	}
	reduced := limit - (attempt - 1)
	if reduced < 2 {
		reduced = 2
	}
	return reduced
}

func adaptivePlannerBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(attempt*attempt) * 250 * time.Millisecond
	if delay > 3*time.Second {
		delay = 3 * time.Second
	}
	return delay
}

func adaptivePlannerMaxTokens(cfg config.Config, attempt int) int {
	_, base := cfg.ResolveLLMRoleOptions("planner", 0.05, 1800)
	if base <= 0 {
		base = 1800
	}
	switch {
	case attempt <= 2:
		return base
	case attempt == 3:
		return maxInt(base, 2400)
	case attempt == 4:
		return maxInt(base, 2800)
	case attempt == 5:
		return maxInt(base, 3200)
	default:
		return maxInt(base, 3600)
	}
}

func persistPlannerAttemptDiagnostics(sessionsDir, runID string, attempts []plannerAttemptDiagnostic) (string, error) {
	if strings.TrimSpace(sessionsDir) == "" || strings.TrimSpace(runID) == "" || len(attempts) == 0 {
		return "", nil
	}
	baseDir := filepath.Join(sessionsDir, "planner-attempts", runID)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", err
	}
	manifest := make([]plannerAttemptDiagnostic, 0, len(attempts))
	for _, attempt := range attempts {
		record := attempt
		record.RawResponse = ""
		record.ExtractedJSON = ""
		prefix := fmt.Sprintf("attempt-%02d", record.Attempt)
		if raw := strings.TrimSpace(attempt.RawResponse); raw != "" {
			rawPath := filepath.Join(baseDir, prefix+".raw.txt")
			if err := os.WriteFile(rawPath, []byte(raw+"\n"), 0o644); err != nil {
				return "", err
			}
		}
		if extracted := strings.TrimSpace(attempt.ExtractedJSON); extracted != "" {
			extractedPath := filepath.Join(baseDir, prefix+".extracted.json")
			if err := os.WriteFile(extractedPath, []byte(extracted+"\n"), 0o644); err != nil {
				return "", err
			}
		}
		manifest = append(manifest, record)
	}
	manifestPath := filepath.Join(baseDir, "attempts.json")
	if err := orchestrator.WriteJSONAtomic(manifestPath, map[string]any{
		"run_id":     runID,
		"created_at": time.Now().UTC(),
		"attempts":   manifest,
	}); err != nil {
		return "", err
	}
	return manifestPath, nil
}

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

func plannerPlaybookHints(goal string, constraints []string, cfg config.Config) (string, []string) {
	query := plannerPlaybookQuery(goal, constraints)
	if query == "" {
		return "", nil
	}
	maxEntries, maxLines := plannerPlaybookBounds(cfg)
	if maxEntries <= 0 || maxLines <= 0 {
		return "", nil
	}
	entries, err := playbook.Load(plannerPlaybookDir())
	if err != nil || len(entries) == 0 {
		return "", nil
	}
	matches := playbook.Match(entries, query, maxEntries)
	if len(matches) == 0 {
		return "", nil
	}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match.Name)
	}
	return strings.TrimSpace(playbook.Render(matches, maxLines)), compactStrings(names)
}

func plannerPlaybookQuery(goal string, constraints []string) string {
	parts := make([]string, 0, len(constraints)+1)
	if trimmed := normalizeGoal(goal); trimmed != "" {
		parts = append(parts, trimmed)
	}
	for _, constraint := range constraints {
		if trimmed := strings.TrimSpace(constraint); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}

func plannerPlaybookBounds(cfg config.Config) (int, int) {
	maxEntries := cfg.Context.PlaybookMax
	if maxEntries == 0 {
		return 0, 0
	}
	if maxEntries < 0 {
		maxEntries = defaultPlannerPlaybookMax
	}
	maxLines := cfg.Context.PlaybookLines
	if maxLines <= 0 {
		maxLines = defaultPlannerPlaybookLines
	}
	return maxEntries, maxLines
}

func plannerPlaybookDir() string {
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return filepath.Join("docs", "playbooks")
	}
	current := wd
	for {
		candidate := filepath.Join(current, "docs", "playbooks")
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return filepath.Join("docs", "playbooks")
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
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
