package workerplan

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Mode string

const (
	ModeConversation     Mode = "conversation"
	ModeDirectExecution  Mode = "direct_execution"
	ModePlannedExecution Mode = "planned_execution"
)

type Plan struct {
	Mode             Mode     `json:"mode"`
	WorkerGoal       string   `json:"worker_goal"`
	PlanSummary      string   `json:"plan_summary"`
	PlanSteps        []string `json:"plan_steps"`
	ActiveStep       string   `json:"active_step"`
	ReplanConditions []string `json:"replan_conditions"`
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
	Parsed      Plan
	Validation  ValidationReport
	Accepted    bool
	FinalError  string
}

func Parse(s string) (Plan, error) {
	cleaned, err := extractJSON(strings.TrimSpace(s))
	if err != nil {
		return Plan{}, err
	}
	var p Plan
	if err := json.Unmarshal([]byte(cleaned), &p); err != nil {
		return Plan{}, fmt.Errorf("parse worker plan json: %w", err)
	}
	return p, nil
}

func Validate(p Plan) ValidationReport {
	var report ValidationReport
	mode := strings.TrimSpace(string(p.Mode))
	switch Mode(mode) {
	case ModeConversation:
		if len(p.PlanSteps) != 0 {
			report.add("conversation mode must not emit plan_steps")
		}
	case ModeDirectExecution:
		if len(p.PlanSteps) != 0 {
			report.add("direct_execution mode must not emit plan_steps")
		}
	case ModePlannedExecution:
		if strings.TrimSpace(p.WorkerGoal) == "" {
			report.add("planned_execution requires worker_goal")
		}
		if len(p.PlanSteps) == 0 {
			report.add("planned_execution requires plan_steps")
		}
		if len(p.PlanSteps) > 6 {
			report.add("planned_execution must remain short")
		}
		if strings.TrimSpace(p.ActiveStep) == "" {
			report.add("planned_execution requires active_step")
		}
		activeFound := false
		seen := map[string]struct{}{}
		for _, step := range p.PlanSteps {
			step = strings.TrimSpace(step)
			if step == "" {
				report.add("plan_steps must not contain empty values")
				continue
			}
			if _, ok := seen[step]; ok {
				report.add("plan_steps must not contain duplicates")
			}
			seen[step] = struct{}{}
			if step == strings.TrimSpace(p.ActiveStep) {
				activeFound = true
			}
			if containsProceduralMarkers(step) {
				report.add("plan_steps must stay semantic rather than command-like")
			}
		}
		if !activeFound {
			report.add("active_step must match one of the plan_steps")
		}
	default:
		report.add("unsupported worker planner mode")
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

func containsProceduralMarkers(step string) bool {
	markers := []string{"|", "&&", ";", ">", "<", "`", "$("}
	for _, m := range markers {
		if strings.Contains(step, m) {
			return true
		}
	}
	lower := strings.ToLower(step)
	badPrefixes := []string{"ls ", "cat ", "grep ", "find ", "nmap ", "curl ", "wget ", "unzip ", "file ", "zipinfo "}
	for _, prefix := range badPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
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
