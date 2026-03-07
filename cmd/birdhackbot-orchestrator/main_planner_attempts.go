package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
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
	RequestPayload    string    `json:"-"`
	RawResponse       string    `json:"-"`
	ExtractedJSON     string    `json:"-"`
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
		record.RequestPayload = ""
		prefix := fmt.Sprintf("attempt-%02d", record.Attempt)
		if request := strings.TrimSpace(attempt.RequestPayload); request != "" {
			requestPath := filepath.Join(baseDir, prefix+".request.json")
			if err := os.WriteFile(requestPath, []byte(request+"\n"), 0o644); err != nil {
				return "", err
			}
		}
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
