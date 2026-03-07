package assist

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type FallbackAssistant struct{}

func (FallbackAssistant) Suggest(_ context.Context, input Input) (Suggestion, error) {
	if isConversationalGoal(input.Goal) {
		return normalizeSuggestion(Suggestion{
			Type:     "complete",
			Decision: "step_complete",
			Final:    "I can help with authorized lab security testing: recon, scanning, controlled validation, artifact analysis, and report drafting. Share a target (IP/host/URL/path) and goal, and I will plan and execute step by step.",
			Risk:     "low",
		}), nil
	}
	return normalizeSuggestion(fallbackQuestionSuggestion(input)), nil
}

func fallbackQuestionSuggestion(input Input) Suggestion {
	question := "The primary assistant response was unavailable or unusable. Retry when the LLM is available, or provide one narrower next step."
	summary := "Fallback is limited to explicit guidance; it will not invent the next command."
	lowerGoal := strings.ToLower(strings.TrimSpace(input.Goal))

	switch {
	case strings.EqualFold(strings.TrimSpace(input.Mode), "recover"):
		question = "The primary assistant response was unavailable during recovery. Retry the LLM step with the latest execution evidence, or provide one narrower corrective action."
		summary = "Recover-mode fallback will not invent a new command."
	case strings.TrimSpace(input.Goal) == "", lowerGoal == "continue", len(input.Targets) == 0 && extractLikelyPath(input.Goal) == "":
		question = "I need one concrete target to continue. Share an IP/hostname/URL or a local file path."
		summary = "Awaiting actionable target."
	case isReportGoal(input.Goal):
		question = "The report step needs an LLM decision. Retry when the LLM is available, or specify whether to finalize from existing evidence or stop."
		summary = "Report fallback will not regenerate report commands."
	case isLocalFileGoal(input.Goal):
		question = "The file-analysis step needs an LLM decision. Retry when the LLM is available, or specify one narrower file action to attempt."
		summary = "Local-workflow fallback will not invent cracking or inspection commands."
	case hasWebTarget(input.Goal):
		question = "The web-analysis step needs an LLM decision. Retry when the LLM is available, or provide one narrower web action to attempt."
		summary = "Web fallback will not invent browse/crawl commands."
	case len(input.Targets) > 0:
		question = "The next action needs an LLM decision. Retry when the LLM is available, or provide one narrower action for the current target."
		summary = "Target is known, but fallback will not invent the next command."
	}

	return Suggestion{
		Type:     "question",
		Decision: "ask_user",
		Question: question,
		Summary:  summary,
		Risk:     "low",
	}
}

func hasWebTarget(goal string) bool {
	_, ok := extractWebTarget(goal)
	return ok
}

func latestRecoverFallbackSuggestion(input Input) (Suggestion, bool) {
	if !strings.EqualFold(strings.TrimSpace(input.Mode), "recover") {
		return Suggestion{}, false
	}
	preferred := preferredFallbackPath(input, isLikelyLocalWorkflowInput(input))
	if preferred == "" {
		return Suggestion{}, false
	}
	return fallbackSuggestionForPath(preferred)
}

func preferredFallbackPath(input Input, allowLog bool) string {
	for _, ref := range input.LatestEvidenceRefs {
		candidate := cleanGoalToken(strings.TrimSpace(ref))
		if candidate == "" {
			continue
		}
		if !allowLog && looksLikeWorkerLogPath(candidate) {
			continue
		}
		return filepath.Clean(candidate)
	}
	if allowLog {
		if candidate := cleanGoalToken(strings.TrimSpace(input.LatestLogPath)); candidate != "" {
			return filepath.Clean(candidate)
		}
	}
	if allowLog {
		for _, ref := range input.LatestInputRefs {
			candidate := cleanGoalToken(strings.TrimSpace(ref))
			if candidate == "" {
				continue
			}
			if looksLikeWorkerLogPath(candidate) {
				continue
			}
			return filepath.Clean(candidate)
		}
	}
	return ""
}

func isLikelyLocalWorkflowInput(input Input) bool {
	corpus := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		input.Goal,
		input.Summary,
		input.RecentLog,
		input.ChatHistory,
		strings.Join(input.KnownFacts, "\n"),
	}, "\n")))
	if corpus == "" {
		return false
	}
	if isLocalFileGoal(input.Goal) {
		return true
	}
	localHints := []string{
		".zip", ".7z", ".tar", ".gz",
		"zip2john", "john", "fcrackzip", "unzip", "recovered_password",
		"local workspace",
		"/home/", "./", "../",
	}
	for _, hint := range localHints {
		if strings.Contains(corpus, hint) {
			return true
		}
	}
	return false
}

func localWorkflowFallbackSuggestion(input Input) (Suggestion, bool) {
	if !isLikelyLocalWorkflowInput(input) {
		return Suggestion{}, false
	}
	return fallbackQuestionSuggestion(input), true
}

func fallbackSuggestionForPath(path string) (Suggestion, bool) {
	candidate := filepath.Clean(strings.TrimSpace(path))
	if candidate == "" {
		return Suggestion{}, false
	}
	if info, err := os.Stat(candidate); err == nil {
		if info.IsDir() {
			return fallbackQuestionSuggestion(Input{Mode: "recover"}), true
		}
		return fallbackQuestionSuggestion(Input{Mode: "recover"}), true
	}
	return fallbackQuestionSuggestion(Input{Mode: "recover"}), true
}

func isReportGoal(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))
	if goal == "" {
		return false
	}
	if !strings.Contains(goal, "report") {
		return false
	}
	actionHints := []string{"create", "write", "generate", "produce", "draft", "owasp", "summary"}
	for _, hint := range actionHints {
		if strings.Contains(goal, hint) {
			return true
		}
	}
	return false
}

func reportEvidenceAlreadyPresent(input Input) bool {
	corpus := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		input.Summary,
		input.RecentLog,
		strings.Join(input.KnownFacts, "\n"),
	}, "\n")))
	if corpus == "" {
		return false
	}
	return strings.Contains(corpus, "owasp-style security assessment report") ||
		strings.Contains(corpus, "## executive summary") ||
		strings.Contains(corpus, "saved report to")
}

func extractReportPath(goal string) string {
	for _, token := range strings.Fields(goal) {
		candidate := cleanGoalToken(token)
		if candidate == "" || strings.Contains(candidate, "://") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(candidate))
		switch ext {
		case ".md", ".txt", ".html":
			return candidate
		}
	}
	return ""
}

func extractLikelyPath(goal string) string {
	tokens := strings.Fields(goal)
	for _, token := range tokens {
		candidate := cleanGoalToken(token)
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "./") || strings.HasPrefix(candidate, "../") || strings.HasPrefix(candidate, "/") {
			return filepath.Clean(candidate)
		}
		if strings.Contains(candidate, "/") && strings.Contains(candidate, ".") {
			return filepath.Clean(candidate)
		}
		if strings.Contains(candidate, ".") && !strings.Contains(candidate, "://") {
			ext := strings.ToLower(filepath.Ext(candidate))
			switch ext {
			case ".zip", ".txt", ".md", ".json", ".log", ".csv", ".xml", ".html":
				return candidate
			}
		}
	}
	return ""
}

func isPathActionGoal(goal string) bool {
	lower := strings.ToLower(strings.TrimSpace(goal))
	if lower == "" {
		return false
	}
	hints := []string{
		"read", "open", "show", "check", "inspect", "analyze", "summarize", "extract", "crack", "decrypt",
	}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func extractWebTarget(goal string) (string, bool) {
	lowerGoal := strings.ToLower(strings.TrimSpace(goal))
	webHint := strings.Contains(lowerGoal, "http") ||
		strings.Contains(lowerGoal, "url") ||
		strings.Contains(lowerGoal, "website") ||
		strings.Contains(lowerGoal, "web ") ||
		strings.Contains(lowerGoal, "site") ||
		strings.Contains(lowerGoal, "domain")

	tokens := strings.Fields(goal)
	for _, token := range tokens {
		candidate := cleanGoalToken(token)
		if candidate == "" {
			continue
		}
		lower := strings.ToLower(candidate)
		if strings.Contains(lower, "://") {
			return candidate, true
		}
		if strings.HasPrefix(lower, "www.") {
			return candidate, true
		}
		if strings.Contains(lower, ".") && !strings.Contains(lower, "/") && webHint && containsLetter(lower) {
			return candidate, true
		}
	}
	return "", false
}

func containsLetter(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func isConversationalGoal(goal string) bool {
	goal = strings.TrimSpace(strings.ToLower(goal))
	if goal == "" {
		return false
	}
	if strings.Contains(goal, "?") {
		if strings.Contains(goal, "scan") || strings.Contains(goal, "exploit") || strings.Contains(goal, "run ") {
			return false
		}
		return true
	}
	prefixes := []string{
		"hello", "hi", "hey", "thanks", "thank you", "who are you", "what can you help", "help me understand",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(goal, prefix) {
			return true
		}
	}
	return false
}

func isLocalFileGoal(goal string) bool {
	goal = strings.TrimSpace(strings.ToLower(goal))
	if goal == "" {
		return false
	}
	fileHints := []string{
		".zip", ".7z", ".tar", ".gz", "file", "folder", "directory", "path", "readme", "current folder", "this folder",
	}
	actionHints := []string{
		"open", "read", "show", "list", "inspect", "extract", "content", "contents", "password", "crack", "decrypt",
	}
	hasFileHint := false
	for _, hint := range fileHints {
		if strings.Contains(goal, hint) {
			hasFileHint = true
			break
		}
	}
	if !hasFileHint {
		return false
	}
	for _, hint := range actionHints {
		if strings.Contains(goal, hint) {
			return true
		}
	}
	return false
}

func cleanGoalToken(token string) string {
	candidate := strings.Trim(token, "\"'`")
	candidate = strings.Trim(candidate, "()[]{}<>")
	candidate = strings.TrimRight(candidate, ",;:!?")
	candidate = strings.TrimRight(candidate, ".")
	return strings.TrimSpace(candidate)
}

func looksLikeWorkerLogPath(path string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	return strings.HasPrefix(base, "worker-") && strings.HasSuffix(base, ".log")
}
