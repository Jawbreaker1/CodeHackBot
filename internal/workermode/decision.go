package workermode

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
)

type Decision struct {
	Mode   workerplan.Mode `json:"mode"`
	Reason string          `json:"reason"`
}

type ValidationIssue struct {
	Message string
}

type ValidationReport struct {
	Issues []ValidationIssue
}

type AttemptRecord struct {
	Prompt      string
	RawResponse string
	Parsed      Decision
	Validation  ValidationReport
	Accepted    bool
	FinalError  string
}

func Parse(s string) (Decision, error) {
	cleaned, err := extractJSON(strings.TrimSpace(s))
	if err != nil {
		return Decision{}, err
	}
	var d Decision
	if err := json.Unmarshal([]byte(cleaned), &d); err != nil {
		return Decision{}, fmt.Errorf("parse worker mode json: %w", err)
	}
	return d, nil
}

func Validate(d Decision) ValidationReport {
	var report ValidationReport
	switch workerplan.Mode(strings.TrimSpace(string(d.Mode))) {
	case workerplan.ModeConversation, workerplan.ModeDirectExecution, workerplan.ModePlannedExecution:
	default:
		report.add("unsupported worker mode")
	}
	if strings.TrimSpace(d.Reason) == "" {
		report.add("worker mode requires reason")
	}
	if containsJSONLikeNoise(d.Reason) {
		report.add("worker mode reason must be a short explanation, not structured output")
	}
	if strings.Count(d.Reason, "\n") > 2 || len(strings.Fields(d.Reason)) > 40 {
		report.add("worker mode reason must remain concise")
	}
	return report
}

func (r ValidationReport) Valid() bool { return len(r.Issues) == 0 }

func (r ValidationReport) Error() error {
	if len(r.Issues) == 0 {
		return nil
	}
	parts := make([]string, 0, len(r.Issues))
	for _, issue := range r.Issues {
		parts = append(parts, issue.Message)
	}
	return fmt.Errorf(strings.Join(parts, "; "))
}

func FallbackDecision() Decision {
	return Decision{
		Mode:   workerplan.ModeConversation,
		Reason: "classifier invalid or ambiguous; defaulting to safe conversation mode",
	}
}

func (r *ValidationReport) add(msg string) {
	r.Issues = append(r.Issues, ValidationIssue{Message: msg})
}

func containsJSONLikeNoise(s string) bool {
	s = strings.TrimSpace(s)
	return strings.ContainsAny(s, "{}[]") || strings.Contains(s, `"mode"`) || strings.Contains(s, `"reason"`)
}

func extractJSON(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty response")
	}
	if idx := strings.LastIndex(s, "</think>"); idx >= 0 {
		s = strings.TrimSpace(s[idx+len("</think>"):])
	}
	s = strings.TrimSpace(strings.TrimPrefix(s, "```json"))
	s = strings.TrimSpace(strings.TrimPrefix(s, "```"))
	s = strings.TrimSpace(strings.TrimSuffix(s, "```"))
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < start {
		return "", fmt.Errorf("no json object found")
	}
	return s[start : end+1], nil
}
