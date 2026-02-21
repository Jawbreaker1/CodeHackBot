package orchestrator

import (
	"context"
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

func buildWorkerAssistant() (string, assist.Assistant, error) {
	cfg, err := loadWorkerLLMConfig()
	if err != nil {
		return "", nil, err
	}
	model := strings.TrimSpace(cfg.LLM.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.Agent.Model)
	}
	if model == "" {
		return "", nil, fmt.Errorf("llm model is required (set %s or config llm.model)", workerLLMModelEnv)
	}
	client := llm.NewLMStudioClient(cfg)
	assistTemp, assistTokens := cfg.ResolveLLMRoleOptions("assist", 0.15, 1200)
	recoveryTemp, recoveryTokens := cfg.ResolveLLMRoleOptions("recovery", 0.1, 900)
	return model, assist.ChainedAssistant{
		Primary: assist.LLMAssistant{
			Client:            client,
			Model:             model,
			Temperature:       float32Ptr(assistTemp),
			MaxTokens:         intPtrPositive(assistTokens),
			RepairTemperature: float32Ptr(recoveryTemp),
			RepairMaxTokens:   intPtrPositive(recoveryTokens),
		},
		Fallback: assist.FallbackAssistant{},
	}, nil
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
