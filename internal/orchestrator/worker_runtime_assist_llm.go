package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type workerAssistMode string

const (
	workerAssistModeStrict   workerAssistMode = "strict"
	workerAssistModeDegraded workerAssistMode = "degraded"
)

type workerAssistantTurnMeta struct {
	Model           string
	AssistMode      string
	ParseRepairUsed bool
	FallbackUsed    bool
	FallbackReason  string
}

type workerAssistant interface {
	Suggest(ctx context.Context, input assist.Input) (assist.Suggestion, workerAssistantTurnMeta, error)
}

type workerAssistRuntime struct {
	mode     workerAssistMode
	model    string
	primary  assist.LLMAssistant
	fallback assist.Assistant
}

func buildWorkerAssistant() (string, string, workerAssistant, error) {
	cfg, err := loadWorkerLLMConfig()
	if err != nil {
		return "", "", nil, err
	}
	model := strings.TrimSpace(cfg.LLM.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.Agent.Model)
	}
	if model == "" {
		return "", "", nil, fmt.Errorf("llm model is required (set %s or config llm.model)", workerLLMModelEnv)
	}
	mode, modeErr := normalizeWorkerAssistMode(cfg.Agent.WorkerAssistMode)
	if modeErr != nil {
		return "", "", nil, modeErr
	}
	client := llm.NewLMStudioClient(cfg)
	assistTemp, assistTokens := cfg.ResolveLLMRoleOptions("assist", 0.15, 1200)
	recoveryTemp, recoveryTokens := cfg.ResolveLLMRoleOptions("recovery", 0.1, 900)
	runtime := &workerAssistRuntime{
		mode:  mode,
		model: model,
		primary: assist.LLMAssistant{
			Client:            client,
			Model:             model,
			Temperature:       float32Ptr(assistTemp),
			MaxTokens:         intPtrPositive(assistTokens),
			RepairTemperature: float32Ptr(recoveryTemp),
			RepairMaxTokens:   intPtrPositive(recoveryTokens),
		},
	}
	if mode == workerAssistModeDegraded {
		runtime.fallback = assist.FallbackAssistant{}
	}
	return model, string(mode), runtime, nil
}

func (r *workerAssistRuntime) Suggest(ctx context.Context, input assist.Input) (assist.Suggestion, workerAssistantTurnMeta, error) {
	meta := workerAssistantTurnMeta{
		Model:      strings.TrimSpace(r.model),
		AssistMode: string(r.mode),
	}
	parseRepairUsed := false
	primary := r.primary
	primary.OnSuggestMeta = func(primaryMeta assist.LLMSuggestMetadata) {
		parseRepairUsed = primaryMeta.ParseRepairUsed
		if model := strings.TrimSpace(primaryMeta.Model); model != "" {
			meta.Model = model
		}
	}
	suggestion, primaryErr := primary.Suggest(ctx, input)
	meta.ParseRepairUsed = parseRepairUsed
	if primaryErr == nil && strings.TrimSpace(suggestion.Type) != "" {
		return suggestion, meta, nil
	}
	if r.mode == workerAssistModeStrict || r.fallback == nil {
		if primaryErr != nil {
			return assist.Suggestion{}, meta, primaryErr
		}
		return assist.Suggestion{}, meta, fmt.Errorf("assistant returned empty suggestion type")
	}

	meta.FallbackUsed = true
	if primaryErr != nil {
		meta.FallbackReason = classifyAssistFallbackReason(primaryErr)
	} else {
		meta.FallbackReason = "empty_primary_suggestion"
	}
	fallbackSuggestion, fallbackErr := r.fallback.Suggest(ctx, input)
	if fallbackErr != nil {
		if primaryErr != nil {
			return assist.Suggestion{}, meta, fmt.Errorf("primary suggestion failed: %w; fallback failed: %v", primaryErr, fallbackErr)
		}
		return assist.Suggestion{}, meta, fallbackErr
	}
	if strings.TrimSpace(fallbackSuggestion.Type) == "" {
		return assist.Suggestion{}, meta, fmt.Errorf("fallback assistant returned empty suggestion type")
	}
	return fallbackSuggestion, meta, nil
}

func classifyAssistFallbackReason(err error) string {
	if err == nil {
		return ""
	}
	var parseErr assist.SuggestionParseError
	if errors.As(err, &parseErr) {
		return "parse_failure"
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout") {
		return "timeout"
	}
	return "primary_error"
}

func normalizeWorkerAssistMode(raw string) (workerAssistMode, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case string(workerAssistModeStrict):
		return workerAssistModeStrict, nil
	case "", string(workerAssistModeDegraded):
		return workerAssistModeDegraded, nil
	default:
		return "", fmt.Errorf("invalid worker assist mode %q (expected strict or degraded)", strings.TrimSpace(raw))
	}
}

func loadWorkerLLMConfig() (config.Config, error) {
	loaded := config.Config{}
	loaded.LLM.TimeoutSeconds = 120

	loadPath := strings.TrimSpace(os.Getenv(workerConfigPathEnv))
	if loadPath == "" {
		pwd := strings.TrimSpace(os.Getenv("PWD"))
		if pwd != "" {
			candidate := filepath.Join(pwd, "config", "default.json")
			if _, err := os.Stat(candidate); err == nil {
				loadPath = candidate
			}
		}
	}
	if loadPath != "" {
		cfg, _, err := config.Load(loadPath, "", "")
		if err != nil {
			return config.Config{}, fmt.Errorf("load config from %s: %w", loadPath, err)
		}
		loaded = cfg
		if loaded.LLM.TimeoutSeconds <= 0 {
			loaded.LLM.TimeoutSeconds = 120
		}
	}

	if v := strings.TrimSpace(os.Getenv(workerLLMBaseURLEnv)); v != "" {
		loaded.LLM.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv(workerLLMModelEnv)); v != "" {
		loaded.LLM.Model = v
	}
	if v := strings.TrimSpace(os.Getenv(workerLLMAPIKeyEnv)); v != "" {
		loaded.LLM.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv(workerLLMTimeoutSeconds)); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			loaded.LLM.TimeoutSeconds = parsed
		}
	}
	if v := strings.TrimSpace(os.Getenv(workerAssistModeEnv)); v != "" {
		loaded.Agent.WorkerAssistMode = v
	}
	return loaded, nil
}

func errorsIsTimeout(err error) bool {
	return err == errWorkerCommandTimeout || strings.Contains(strings.ToLower(err.Error()), "timeout")
}

func isAssistTimeoutError(suggestErr error, suggestCtxErr error, runCtxErr error) bool {
	if suggestErr == nil {
		return false
	}
	if suggestCtxErr == context.DeadlineExceeded || runCtxErr == context.DeadlineExceeded {
		return true
	}
	lower := strings.ToLower(suggestErr.Error())
	return strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "timeout")
}

func newAssistCallContext(runCtx context.Context) (context.Context, context.CancelFunc, time.Duration, time.Duration, error) {
	remaining := workerAssistLLMCallMax
	if deadline, ok := runCtx.Deadline(); ok {
		remaining = time.Until(deadline)
	}
	if remaining <= 2*time.Second {
		remainingSecs := int(remaining.Seconds())
		if remainingSecs < 0 {
			remainingSecs = 0
		}
		return runCtx, func() {}, 0, remaining, fmt.Errorf("assist call timeout: remaining budget too low (%ds)", remainingSecs)
	}
	callTimeout := workerAssistLLMCallMax
	available := remaining - workerAssistBudgetReserve
	if available > 0 && available < callTimeout {
		callTimeout = available
	}
	if available <= 0 {
		callTimeout = remaining - time.Second
	}
	if callTimeout > remaining-time.Second {
		callTimeout = remaining - time.Second
	}
	if callTimeout < 3*time.Second {
		callTimeout = minDuration(remaining-time.Second, workerAssistLLMCallMin)
	}
	if callTimeout < 3*time.Second {
		callTimeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(runCtx, callTimeout)
	return ctx, cancel, callTimeout, remaining, nil
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
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
