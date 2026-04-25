package workertask

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Action string

const (
	ActionContinueActiveTask Action = "continue_active_task"
	ActionStartNewTask       Action = "start_new_task"
)

type Decision struct {
	Action Action `json:"action"`
	Reason string `json:"reason"`
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
		return Decision{}, fmt.Errorf("parse task boundary json: %w", err)
	}
	return d, nil
}

func Validate(d Decision) ValidationReport {
	var report ValidationReport
	switch Action(strings.TrimSpace(string(d.Action))) {
	case ActionContinueActiveTask, ActionStartNewTask:
	default:
		report.add("unsupported task boundary action")
	}
	if strings.TrimSpace(d.Reason) == "" {
		report.add("task boundary action requires reason")
	}
	if containsJSONLikeNoise(d.Reason) {
		report.add("task boundary reason must be a short explanation, not structured output")
	}
	if strings.Count(d.Reason, "\n") > 2 || len(strings.Fields(d.Reason)) > 40 {
		report.add("task boundary reason must remain concise")
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
	return errors.New(strings.Join(parts, "; "))
}

func FallbackDecision() Decision {
	return Decision{
		Action: ActionStartNewTask,
		Reason: "boundary classifier invalid or ambiguous; defaulting to a fresh task",
	}
}

func (r *ValidationReport) add(msg string) {
	r.Issues = append(r.Issues, ValidationIssue{Message: msg})
}

func containsJSONLikeNoise(s string) bool {
	s = strings.TrimSpace(s)
	return strings.ContainsAny(s, "{}[]") || strings.Contains(s, `"action"`) || strings.Contains(s, `"reason"`)
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
