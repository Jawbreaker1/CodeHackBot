package workerstep

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusSatisfied  Status = "satisfied"
	StatusBlocked    Status = "blocked"
)

type Evaluation struct {
	Status  Status `json:"status"`
	Reason  string `json:"reason"`
	Summary string `json:"summary"`
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
	Parsed      Evaluation
	Validation  ValidationReport
	Accepted    bool
	FinalError  string
}

func Parse(s string) (Evaluation, error) {
	cleaned, err := extractJSON(strings.TrimSpace(s))
	if err != nil {
		return Evaluation{}, err
	}
	var e Evaluation
	if err := json.Unmarshal([]byte(cleaned), &e); err != nil {
		return Evaluation{}, fmt.Errorf("parse step evaluation json: %w", err)
	}
	return e, nil
}

func Validate(e Evaluation) ValidationReport {
	var report ValidationReport
	switch Status(strings.TrimSpace(string(e.Status))) {
	case StatusInProgress, StatusSatisfied, StatusBlocked:
	default:
		report.add("unsupported step evaluation status")
	}
	if strings.TrimSpace(e.Reason) == "" {
		report.add("step evaluation requires reason")
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

func (r *ValidationReport) add(msg string) {
	r.Issues = append(r.Issues, ValidationIssue{Message: msg})
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
