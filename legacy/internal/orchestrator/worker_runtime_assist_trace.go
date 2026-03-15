package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

type workerAssistLLMTraceRecord struct {
	Turn                int      `json:"turn"`
	Mode                string   `json:"mode"`
	AssistMode          string   `json:"assist_mode"`
	Model               string   `json:"model"`
	ParseRepairUsed     bool     `json:"parse_repair_used"`
	FallbackUsed        bool     `json:"fallback_used"`
	FallbackReason      string   `json:"fallback_reason,omitempty"`
	PrimaryFinishReason string   `json:"primary_finish_reason,omitempty"`
	RepairFinishReason  string   `json:"repair_finish_reason,omitempty"`
	PrimaryResponse     string   `json:"primary_response,omitempty"`
	PrimaryTruncated    bool     `json:"primary_truncated,omitempty"`
	RepairResponse      string   `json:"repair_response,omitempty"`
	RepairTruncated     bool     `json:"repair_truncated,omitempty"`
	SuggestionType      string   `json:"suggestion_type,omitempty"`
	SuggestionSummary   string   `json:"suggestion_summary,omitempty"`
	SuggestionCommand   string   `json:"suggestion_command,omitempty"`
	SuggestionArgs      []string `json:"suggestion_args,omitempty"`
	SuggestionFinal     string   `json:"suggestion_final,omitempty"`
	SuggestionQuestion  string   `json:"suggestion_question,omitempty"`
	ExecutionError      string   `json:"execution_error,omitempty"`
}

func writeWorkerAssistLLMTrace(cfg WorkerRunConfig, turn int, mode string, turnMeta workerAssistantTurnMeta, suggestion assist.Suggestion, suggestErr error) (string, error) {
	if !turnMeta.TraceEnabled {
		return "", nil
	}
	primary := strings.TrimSpace(turnMeta.PrimaryResponse)
	repair := strings.TrimSpace(turnMeta.RepairResponse)
	if primary == "" && repair == "" {
		return "", nil
	}
	primary, primaryTruncated := clampTraceText(primary, workerAssistTraceResponseMaxChars)
	repair, repairTruncated := clampTraceText(repair, workerAssistTraceResponseMaxChars)
	record := workerAssistLLMTraceRecord{
		Turn:                turn,
		Mode:                strings.TrimSpace(mode),
		AssistMode:          strings.TrimSpace(turnMeta.AssistMode),
		Model:               strings.TrimSpace(turnMeta.Model),
		ParseRepairUsed:     turnMeta.ParseRepairUsed,
		FallbackUsed:        turnMeta.FallbackUsed,
		FallbackReason:      strings.TrimSpace(turnMeta.FallbackReason),
		PrimaryFinishReason: strings.TrimSpace(turnMeta.PrimaryFinishReason),
		RepairFinishReason:  strings.TrimSpace(turnMeta.RepairFinishReason),
		PrimaryResponse:     primary,
		PrimaryTruncated:    primaryTruncated,
		RepairResponse:      repair,
		RepairTruncated:     repairTruncated,
		SuggestionType:      strings.TrimSpace(suggestion.Type),
		SuggestionSummary:   strings.TrimSpace(suggestion.Summary),
		SuggestionCommand:   strings.TrimSpace(suggestion.Command),
		SuggestionArgs:      compactStrings(suggestion.Args),
		SuggestionFinal:     strings.TrimSpace(suggestion.Final),
		SuggestionQuestion:  strings.TrimSpace(suggestion.Question),
	}
	if suggestErr != nil {
		record.ExecutionError = strings.TrimSpace(suggestErr.Error())
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal llm trace: %w", err)
	}
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", fmt.Errorf("create llm trace dir: %w", err)
	}
	path := filepath.Join(artifactDir, sanitizePathComponent(fmt.Sprintf("%s-a%d-t%02d-llm-response.json", cfg.WorkerID, cfg.Attempt, turn)))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write llm trace: %w", err)
	}
	return path, nil
}

func clampTraceText(text string, maxChars int) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if maxChars <= 0 || len(trimmed) <= maxChars {
		return trimmed, false
	}
	head := (maxChars * 2) / 3
	tail := maxChars - head - 3
	if tail < 0 {
		tail = 0
	}
	return trimmed[:head] + "..." + trimmed[len(trimmed)-tail:], true
}
