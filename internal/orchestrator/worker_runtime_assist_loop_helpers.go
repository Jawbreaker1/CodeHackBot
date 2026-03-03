package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func trackAssistActionStreak(lastKey string, lastStreak int, currentKey string) (string, int) {
	currentKey = strings.TrimSpace(currentKey)
	if currentKey == "" {
		return "", 0
	}
	if currentKey == lastKey {
		return currentKey, lastStreak + 1
	}
	return currentKey, 1
}

func trackAssistResultStreak(lastKey string, lastStreak int, command string, args []string, runErr error, output []byte) (string, int) {
	currentKey := buildAssistResultKey(command, args, runErr, output)
	if currentKey == "" {
		return "", 0
	}
	if currentKey == lastKey {
		return currentKey, lastStreak + 1
	}
	return currentKey, 1
}

func buildAssistResultKey(command string, args []string, runErr error, output []byte) string {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return ""
	}
	normalizedArgs := normalizeArgs(args)
	status := "ok"
	if runErr != nil {
		status = "err:" + strings.TrimSpace(runErr.Error())
	}
	fragment := strings.Join(strings.Fields(string(capBytes(output, workerAssistResultFingerprintBytes))), " ")
	return strings.Join([]string{command, strings.Join(normalizedArgs, " "), status, fragment}, "|")
}

func isAssistSummaryTask(task TaskSpec) bool {
	strategy := strings.ToLower(strings.TrimSpace(task.Strategy))
	switch strategy {
	case "summarize_and_replan", "summary", "report_summary":
		return true
	}
	if strings.EqualFold(strings.TrimSpace(task.TaskID), "task-plan-summary") {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(task.Title))
	return strings.Contains(title, "plan synthesis summary")
}

func isAssistAdaptiveReplanTask(task TaskSpec) bool {
	strategy := strings.ToLower(strings.TrimSpace(task.Strategy))
	return strings.HasPrefix(strategy, "adaptive_replan_")
}

func isAssistReconOrValidationTask(task TaskSpec) bool {
	strategy := strings.ToLower(strings.TrimSpace(task.Strategy))
	if strategy != "" {
		keywords := []string{
			"recon",
			"hypothesis_validate",
			"validate",
			"inventory",
			"discovery",
			"service_enum",
			"vuln_mapping",
		}
		for _, keyword := range keywords {
			if strings.Contains(strategy, keyword) {
				return true
			}
		}
	}
	text := strings.ToLower(strings.TrimSpace(task.Title + " " + task.Goal))
	if text == "" {
		return false
	}
	return strings.Contains(text, "recon") ||
		strings.Contains(text, "discover") ||
		strings.Contains(text, "inventory") ||
		strings.Contains(text, "validate")
}

func allowAssistNoNewEvidenceCompletion(task TaskSpec) bool {
	if isAssistSummaryTask(task) || isAssistAdaptiveReplanTask(task) {
		return true
	}
	return !isAssistReconOrValidationTask(task)
}

func isNoNewEvidenceCandidate(command string, args []string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	if isNetworkSensitiveCommand(command) {
		return true
	}
	base := strings.ToLower(filepath.Base(command))
	switch base {
	case "list_dir", "ls", "dir", "read_file", "read":
		return true
	}
	if base != "bash" && base != "sh" && base != "zsh" {
		return false
	}
	if len(args) == 0 {
		return false
	}
	script := strings.TrimSpace(args[0])
	if script == "" || strings.HasPrefix(script, "-") {
		return false
	}
	if strings.Contains(filepath.ToSlash(script), "tools/") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(script))
	switch ext {
	case ".sh", ".bash", ".zsh", ".py", ".rb", ".pl":
		return true
	}
	return false
}

func isLowValueListingCommand(command string, args []string) bool {
	base := canonicalAssistActionCommand(command)
	if base != "list_dir" {
		return false
	}
	_ = args
	return true
}

func resolveRecoverPivotBasis(summary, recoverHint string, observations []string) (basis string, source string, kind string) {
	summary = strings.TrimSpace(summary)
	if summary != "" {
		if hasRecoverUnknownMarker(summary) {
			return summary, "summary", "unknown"
		}
		if anchor := extractRecoverEvidenceAnchor(summary); anchor != "" {
			return anchor, "summary", "evidence"
		}
	}
	if anchor := extractRecoverEvidenceAnchor(recoverHint); anchor != "" {
		return anchor, "recover_hint", "evidence"
	}
	for i := len(observations) - 1; i >= 0 && i >= len(observations)-6; i-- {
		if anchor := extractRecoverEvidenceAnchor(observations[i]); anchor != "" {
			return anchor, "observation", "evidence"
		}
	}
	return "", "", ""
}

func hasRecoverUnknownMarker(summary string) bool {
	lower := strings.ToLower(strings.TrimSpace(summary))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"unknown",
		"hypothesis",
		"uncertain",
		"verify whether",
		"under test",
		"need to determine",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func extractRecoverEvidenceAnchor(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if pathLike := firstPathLikeToken(trimmed); pathLike != "" {
		return pathLike
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{
		"scope denied",
		"permission denied",
		"not found",
		"no such file",
		"timed out",
		"timeout",
		"missing",
		"failed",
		"error",
		"unavailable",
		"exit code",
	} {
		if strings.Contains(lower, marker) {
			return marker
		}
	}
	return ""
}

func firstPathLikeToken(text string) string {
	fields := strings.Fields(text)
	for _, field := range fields {
		token := strings.Trim(field, "\"'`.,;:()[]{}")
		lower := strings.ToLower(token)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			continue
		}
		if !strings.Contains(token, "/") && !strings.Contains(token, "\\") {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(token)); ext != "" {
			return token
		}
		if strings.HasPrefix(token, "/") || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") {
			return token
		}
		if strings.Contains(strings.ToLower(filepath.ToSlash(token)), "/tools/") {
			return token
		}
	}
	return ""
}

func buildAssistPromptScope(runScope Scope, taskTargets []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(runScope.Networks)+len(runScope.Targets)+len(runScope.DenyTargets))
	add := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	for _, network := range runScope.Networks {
		add(network)
	}
	for _, target := range runScope.Targets {
		add(target)
	}
	for _, deny := range runScope.DenyTargets {
		trimmed := strings.TrimSpace(deny)
		if trimmed == "" {
			continue
		}
		add("deny:" + trimmed)
	}
	if len(out) > 0 {
		return out
	}
	for _, target := range taskTargets {
		add(target)
	}
	return out
}

func materializeAssistExpectedArtifacts(cfg WorkerRunConfig, task TaskSpec, sourcePath string) ([]string, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read assist completion log: %w", err)
	}
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, task.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return nil, fmt.Errorf("create assist artifact dir: %w", err)
	}
	out := make([]string, 0, len(task.ExpectedArtifacts))
	for _, raw := range task.ExpectedArtifacts {
		requirement := strings.TrimSpace(raw)
		if requirement == "" || !artifactRequirementNeedsConcreteMatch(requirement) {
			continue
		}
		relative := requirement
		if filepath.IsAbs(relative) {
			relative = filepath.Base(relative)
		}
		relative = filepath.Clean(relative)
		if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			relative = filepath.Base(requirement)
		}
		if strings.TrimSpace(relative) == "" {
			continue
		}
		targetPath := filepath.Join(artifactDir, relative)
		if !pathWithinRoot(targetPath, artifactDir) {
			targetPath = filepath.Join(artifactDir, filepath.Base(relative))
		}
		if strings.EqualFold(filepath.Clean(targetPath), filepath.Clean(sourcePath)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return nil, fmt.Errorf("create expected artifact dir: %w", err)
		}
		if err := os.WriteFile(targetPath, data, 0o644); err != nil {
			return nil, fmt.Errorf("write expected artifact %s: %w", requirement, err)
		}
		out = append(out, targetPath)
	}
	return compactStrings(out), nil
}

func buildAutonomousQuestionAnswer(task TaskSpec, question string, observations []string) string {
	parts := make([]string, 0, 4)
	if trimmed := strings.TrimSpace(question); trimmed != "" {
		parts = append(parts, "question="+trimmed)
	}
	if len(task.Targets) > 0 {
		parts = append(parts, "targets="+strings.Join(task.Targets, ", "))
	}
	if len(task.DoneWhen) > 0 {
		parts = append(parts, "done_when="+strings.Join(task.DoneWhen, "; "))
	}
	if len(observations) > 0 {
		latest := strings.TrimSpace(observations[len(observations)-1])
		if latest != "" {
			if len(latest) > 240 {
				latest = latest[:240] + "..."
			}
			parts = append(parts, "latest_observation="+latest)
		}
	}
	return strings.Join(parts, " | ")
}

func resolveShellToolScriptPath(workDir, command string, args []string) ([]string, string, bool) {
	if len(args) == 0 {
		return args, "", false
	}
	shell := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	switch shell {
	case "bash", "sh", "zsh":
	default:
		return args, "", false
	}
	script := strings.TrimSpace(args[0])
	if script == "" || strings.HasPrefix(script, "-") {
		return args, "", false
	}
	if strings.Contains(script, "/") || strings.Contains(script, string(os.PathSeparator)) {
		return args, "", false
	}
	if !strings.HasSuffix(strings.ToLower(script), ".sh") {
		return args, "", false
	}
	if _, err := os.Stat(filepath.Join(workDir, script)); err == nil {
		return args, "", false
	}
	candidate := filepath.Join(workDir, "tools", script)
	if _, err := os.Stat(candidate); err != nil {
		return args, "", false
	}
	updated := append([]string{}, args...)
	updated[0] = filepath.ToSlash(filepath.Join("tools", script))
	note := fmt.Sprintf("resolved shell script %s to %s from worker tools directory", script, updated[0])
	return updated, note, true
}
