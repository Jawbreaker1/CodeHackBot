package assist

import (
	"context"
	"path/filepath"
	"strings"
	"unicode"
)

type FallbackAssistant struct{}

func (FallbackAssistant) Suggest(_ context.Context, input Input) (Suggestion, error) {
	if isConversationalGoal(input.Goal) {
		return normalizeSuggestion(Suggestion{
			Type:  "complete",
			Final: "I can help with authorized lab security testing: recon, scanning, controlled validation, artifact analysis, and report drafting. Share a target (IP/host/URL/path) and goal, and I will plan and execute step by step.",
			Risk:  "low",
		}), nil
	}
	if path := extractLikelyPath(input.Goal); path != "" && isPathActionGoal(input.Goal) {
		return normalizeSuggestion(Suggestion{
			Type:    "command",
			Command: "read_file",
			Args:    []string{path},
			Summary: "Read the referenced local file to continue.",
			Risk:    "low",
		}), nil
	}
	if isReportGoal(input.Goal) {
		args := []string{}
		if path := extractReportPath(input.Goal); path != "" {
			args = append(args, path)
		}
		return normalizeSuggestion(Suggestion{
			Type:    "command",
			Command: "report",
			Args:    args,
			Summary: "Generate a security report from collected session evidence.",
			Risk:    "low",
		}), nil
	}
	if isLocalFileGoal(input.Goal) {
		lowerGoal := strings.ToLower(strings.TrimSpace(input.Goal))
		if strings.Contains(lowerGoal, "folder") || strings.Contains(lowerGoal, "directory") || strings.Contains(lowerGoal, "current") {
			return normalizeSuggestion(Suggestion{
				Type:    "command",
				Command: "list_dir",
				Args:    []string{"."},
				Summary: "List current directory to locate the target file.",
				Risk:    "low",
			}), nil
		}
		return normalizeSuggestion(Suggestion{
			Type:     "question",
			Question: "The primary LLM response was unavailable or unusable. For local file analysis, share the exact file path/name (for example `./secret.zip`) and any known password or wordlist path, and I will run the next step.",
			Summary:  "Awaiting local file details.",
			Risk:     "low",
		}), nil
	}
	if url, ok := extractWebTarget(input.Goal); ok {
		return normalizeSuggestion(Suggestion{
			Type:    "command",
			Command: "browse",
			Args:    []string{url},
			Summary: "Fetch the target URL for analysis.",
			Risk:    "low",
		}), nil
	}
	if len(input.Targets) == 0 {
		return normalizeSuggestion(Suggestion{
			Type:     "question",
			Question: "I need one concrete target to continue. Share an IP/hostname/URL or a local file path.",
			Summary:  "Awaiting actionable target.",
			Risk:     "low",
		}), nil
	}
	return normalizeSuggestion(Suggestion{
		Type:    "command",
		Command: "nmap",
		Args:    []string{"-sV", "-v", input.Targets[0]},
		Summary: "Run a safe service/version scan on the primary target.",
		Risk:    "low",
	}), nil
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

func extractReportPath(goal string) string {
	for _, token := range strings.Fields(goal) {
		candidate := strings.Trim(token, "\"'`(),;:[]{}<>")
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
		candidate := strings.Trim(token, "\"'()[]{}<>,;:")
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
		candidate := strings.Trim(token, "\"'()[]{}<>,;:")
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
