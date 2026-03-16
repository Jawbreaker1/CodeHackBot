package context

import (
	"regexp"
	"strings"
)

// TaskRuntime is the minimal structured runtime task context for the worker loop.
type TaskRuntime struct {
	State         string
	CurrentTarget string
	MissingFact   string
}

var (
	ipv4Pattern     = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	fileNamePattern = regexp.MustCompile(`(?:\./|/)?[A-Za-z0-9._/-]+\.[A-Za-z0-9]{1,8}\b`)
)

func InitialTaskRuntime(goal string) TaskRuntime {
	target := inferCurrentTarget(goal)
	missing := "next evidence needed to satisfy the goal"
	if target != "" {
		missing = "next evidence needed about " + target
	}
	return TaskRuntime{
		State:         "running",
		CurrentTarget: target,
		MissingFact:   missing,
	}
}

func UpdateTaskRuntime(current TaskRuntime, goal string, latest ExecutionResult) TaskRuntime {
	next := current
	if strings.TrimSpace(next.State) == "" {
		next.State = "running"
	}
	if strings.TrimSpace(next.CurrentTarget) == "" {
		next.CurrentTarget = inferCurrentTarget(goal + " " + latest.Action)
	}
	next.MissingFact = inferMissingFact(goal, next.CurrentTarget, latest)
	return next
}

func inferCurrentTarget(text string) string {
	if match := ipv4Pattern.FindString(text); match != "" {
		return match
	}
	for _, match := range fileNamePattern.FindAllString(text, -1) {
		cleaned := strings.TrimSpace(strings.Trim(match, "\"'"))
		if cleaned == "" {
			continue
		}
		if strings.Contains(cleaned, "sessions/rebuild-dev") {
			continue
		}
		return cleaned
	}
	return ""
}

func inferMissingFact(goal, currentTarget string, latest ExecutionResult) string {
	if hasSignal(latest.Signals, "incorrect_password") {
		if currentTarget != "" {
			return "credential or password required for " + currentTarget
		}
		return "credential or password required to satisfy the goal"
	}
	if hasSignal(latest.Signals, "missing_path") {
		if currentTarget != "" {
			return "correct path or artifact for " + currentTarget
		}
		return "correct path or artifact needed"
	}
	if strings.TrimSpace(currentTarget) != "" {
		return "next evidence needed about " + currentTarget
	}
	if strings.TrimSpace(goal) != "" {
		return "next evidence needed to satisfy the goal"
	}
	return "(none)"
}

func hasSignal(signals []string, want string) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}
