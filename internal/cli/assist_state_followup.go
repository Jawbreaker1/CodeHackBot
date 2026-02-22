package cli

import (
	"errors"
	"path/filepath"
	"strings"
)

func shouldStartBaselineScan(answer string) bool {
	normalized := strings.ToLower(strings.TrimSpace(answer))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "scan") && (strings.Contains(normalized, "start") || strings.Contains(normalized, "first") || strings.HasPrefix(normalized, "yes")) {
		return true
	}
	if strings.Contains(normalized, "non intrusive") && strings.Contains(normalized, "start") {
		return true
	}
	return false
}

func extractHostLikeToken(text string) string {
	candidates := strings.Fields(strings.ToLower(text))
	for _, token := range candidates {
		token = strings.Trim(token, "\"'()[]{}<>.,;:")
		if token == "" {
			continue
		}
		if strings.Contains(token, "/") || strings.Contains(token, ":") {
			continue
		}
		if strings.Count(token, ".") >= 1 && !strings.HasPrefix(token, ".") && !strings.HasSuffix(token, ".") {
			return token
		}
	}
	return ""
}

func isBenignNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	var cmdErr commandError
	if !errors.As(err, &cmdErr) {
		return false
	}
	if cmdErr.Err == nil || !strings.Contains(strings.ToLower(cmdErr.Err.Error()), "exit status 1") {
		return false
	}
	command := strings.ToLower(strings.TrimSpace(cmdErr.Result.Command))
	switch command {
	case "grep", "rg":
		return strings.TrimSpace(cmdErr.Result.Output) == ""
	case "bash", "sh":
		if len(cmdErr.Result.Args) == 0 {
			return false
		}
		joined := strings.ToLower(strings.Join(cmdErr.Result.Args, " "))
		return strings.Contains(joined, "grep") && strings.TrimSpace(cmdErr.Result.Output) == ""
	default:
		return false
	}
}

func isAssistRepeatedGuard(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "assistant loop guard: repeated command blocked") ||
		strings.Contains(lower, "assistant loop guard: repeated question blocked")
}

func shouldTreatPendingInputAsNewGoal(answer, question string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	if isLikelyFollowUpAnswer(answer, question) {
		return false
	}
	return true
}

func isLikelyFollowUpAnswer(answer, question string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	lower := strings.ToLower(answer)
	if shouldUseDefaultChoice(answer, question) {
		return true
	}
	switch lower {
	case "y", "yes", "n", "no", "ok", "okay", "continue", "proceed", "go ahead", "stop", "skip":
		return true
	}
	if extractFirstURL(answer) != "" || extractHostLikeToken(answer) != "" {
		return true
	}
	if looksLikePathOrFilename(answer) {
		return true
	}
	// Short phrases are often direct answers to a clarifying question.
	if !strings.Contains(answer, "?") && len(strings.Fields(answer)) <= 5 {
		return true
	}
	return false
}

func looksLikePathOrFilename(text string) bool {
	token := strings.TrimSpace(text)
	if token == "" {
		return false
	}
	token = strings.Trim(token, "\"'`")
	if strings.HasPrefix(token, "/") || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") {
		return true
	}
	if strings.Contains(token, "/") {
		return true
	}
	base := filepath.Base(token)
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".zip", ".log", ".csv", ".xml", ".html":
		return true
	default:
		return false
	}
}

func shouldUseDefaultChoice(answer, question string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	question = strings.ToLower(strings.TrimSpace(question))
	if answer == "" || question == "" {
		return false
	}
	defaultSignals := []string{"default", "go ahead", "proceed", "use that", "continue", "yes"}
	hasDefault := false
	for _, signal := range defaultSignals {
		if strings.Contains(answer, signal) {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		return false
	}
	optionSignals := []string{"default", "option", "wordlist", "password list", "profile", "mode"}
	for _, signal := range optionSignals {
		if strings.Contains(question, signal) {
			return true
		}
	}
	return false
}

func autoAssistFollowUpAnswer(question string) (string, bool) {
	question = strings.ToLower(strings.TrimSpace(question))
	if question == "" {
		return "", false
	}
	authConfirmHints := []string{
		"is this correct and authorized",
		"is this authorized",
		"within your lab environment",
		"confirm authorization",
	}
	for _, hint := range authConfirmHints {
		if strings.Contains(question, hint) {
			return "Yes. Proceed within the configured in-scope lab targets only.", true
		}
	}
	requiresSpecificInput := []string{
		"which file", "file path", "path", "target", "ip", "hostname", "url", "password",
		"wordlist path", "token", "credential", "username", "scope",
	}
	for _, hint := range requiresSpecificInput {
		if strings.Contains(question, hint) {
			return "", false
		}
	}
	autoContinueHints := []string{
		"default", "proceed", "continue", "go ahead", "do you want me", "should i", "run now", "start now",
	}
	for _, hint := range autoContinueHints {
		if strings.Contains(question, hint) {
			return "go ahead with default", true
		}
	}
	return "", false
}
