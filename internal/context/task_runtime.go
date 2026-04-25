package context

import (
	"os"
	"path/filepath"
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
	target := inferCurrentTarget(goal, "")
	return initialTaskRuntimeFromTarget(target)
}

func InitialTaskRuntimeInDir(goal, cwd string) TaskRuntime {
	target := inferCurrentTarget(goal, cwd)
	return initialTaskRuntimeFromTarget(target)
}

func initialTaskRuntimeFromTarget(target string) TaskRuntime {
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
		next.CurrentTarget = inferCurrentTarget(goal+" "+latest.Action, "")
	}
	next.MissingFact = inferMissingFact(goal, next.CurrentTarget, latest)
	return next
}

func inferCurrentTarget(text, cwd string) string {
	if match := ipv4Pattern.FindString(text); match != "" {
		return match
	}
	for _, match := range fileNamePattern.FindAllString(text, -1) {
		cleaned := strings.TrimSpace(strings.Trim(match, "\"'"))
		if cleaned == "" {
			continue
		}
		if isSessionRuntimeArtifactPath(cleaned) {
			continue
		}
		if resolved, ok := resolveLocalTargetIdentity(cleaned, cwd); ok {
			return resolved
		}
		return cleaned
	}
	return ""
}

func isSessionRuntimeArtifactPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	if strings.HasSuffix(path, "/session.json") || path == "session.json" {
		return true
	}
	if strings.Contains(path, "/logs/") {
		base := filepath.Base(path)
		return strings.HasPrefix(base, "cmd-") && strings.HasSuffix(base, ".log")
	}
	if strings.Contains(path, "/context/") {
		base := filepath.Base(path)
		if strings.HasPrefix(base, "step-") && strings.HasSuffix(base, ".txt") {
			return true
		}
		if strings.HasPrefix(base, "step-") && strings.HasSuffix(base, "-meta.txt") {
			return true
		}
	}
	return false
}

func resolveLocalTargetIdentity(target, cwd string) (string, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}
	if filepath.IsAbs(target) {
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			return filepath.Clean(target), true
		}
		return "", false
	}
	if strings.TrimSpace(cwd) == "" {
		return "", false
	}
	candidate := filepath.Join(cwd, target)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return filepath.Clean(candidate), true
	}
	return "", false
}

func inferMissingFact(goal, currentTarget string, latest ExecutionResult) string {
	if hasSignal(latest.Signals, "incorrect_password") {
		if currentTarget != "" {
			return "credential or password required for " + currentTarget
		}
		return "credential or password required to satisfy the goal"
	}
	if hasSignal(latest.Signals, "missing_path") {
		if missing := inferMissingArtifactIdentity(latest, currentTarget); missing != "" {
			return "correct path or artifact needed: " + missing
		}
		if currentTarget != "" && missingPathMentionsTarget(latest, currentTarget) {
			return "correct path or artifact needed for " + currentTarget
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

func preferredExecutionEvidence(latest ExecutionResult) string {
	if strings.TrimSpace(latest.OutputEvidence) != "" {
		return latest.OutputEvidence
	}
	return latest.OutputSummary
}

func inferMissingArtifactIdentity(latest ExecutionResult, currentTarget string) string {
	actionText := strings.Join([]string{latest.Action, strings.Join(latest.ArtifactRefs, " ")}, " ")
	for _, candidate := range extractPathLikeCandidates(preferredExecutionEvidence(latest)) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if strings.TrimSpace(currentTarget) != "" && sameArtifact(candidate, currentTarget) {
			continue
		}
		if corroboratesMissingArtifact(candidate, actionText) {
			return candidate
		}
	}
	for _, candidate := range append(extractPathLikeCandidates(actionText), extractPathLikeCandidates(strings.Join(latest.LogRefs, " "))...) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if strings.TrimSpace(currentTarget) != "" && sameArtifact(candidate, currentTarget) {
			continue
		}
		return candidate
	}
	return ""
}

func extractPathLikeCandidates(text string) []string {
	matches := fileNamePattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		candidate := strings.TrimSpace(strings.Trim(match, "\"'"))
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func sameArtifact(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	return a == b
}

func corroboratesMissingArtifact(candidate, actionText string) bool {
	candidate = strings.TrimSpace(candidate)
	actionText = strings.TrimSpace(actionText)
	if candidate == "" || actionText == "" {
		return false
	}
	if strings.Contains(actionText, candidate) {
		return true
	}
	base := filepath.Base(candidate)
	return base != "." && base != "/" && strings.Contains(actionText, base)
}

func missingPathMentionsTarget(latest ExecutionResult, currentTarget string) bool {
	currentTarget = strings.TrimSpace(currentTarget)
	if currentTarget == "" {
		return false
	}
	fields := []string{
		latest.Action,
		preferredExecutionEvidence(latest),
		strings.Join(latest.ArtifactRefs, " "),
	}
	for _, field := range fields {
		if strings.Contains(field, currentTarget) {
			return true
		}
	}
	return false
}
