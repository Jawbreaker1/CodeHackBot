package workeraction

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Decision string

const (
	DecisionExecute Decision = "execute"
	DecisionRevise  Decision = "revise"
	DecisionBlocked Decision = "blocked"
)

type Review struct {
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason"`
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
	Parsed      Review
	Validation  ValidationReport
	Accepted    bool
	FinalError  string
}

func Parse(s string) (Review, error) {
	cleaned, err := extractJSON(strings.TrimSpace(s))
	if err != nil {
		return Review{}, err
	}
	var r Review
	if err := json.Unmarshal([]byte(cleaned), &r); err != nil {
		return Review{}, fmt.Errorf("parse action review json: %w", err)
	}
	return r, nil
}

func Validate(r Review) ValidationReport {
	var report ValidationReport
	switch Decision(strings.TrimSpace(string(r.Decision))) {
	case DecisionExecute, DecisionRevise, DecisionBlocked:
	default:
		report.add("unsupported action review decision")
	}
	if strings.TrimSpace(r.Reason) == "" {
		report.add("action review requires reason")
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
