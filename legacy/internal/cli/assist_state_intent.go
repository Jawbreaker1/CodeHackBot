package cli

import "strings"

func isPlaceholderCommand(cmd string) bool {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	switch cmd {
	case "scan", "recon", "enumerate", "probe", "test":
		return true
	default:
		return false
	}
}

func isSummaryIntent(goal string) bool {
	if goal == "" {
		return false
	}
	lower := strings.ToLower(goal)
	hints := []string{"summary", "summarize", "what did you find", "findings", "what is it about", "tell me what", "report"}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func isWriteCreationIntent(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))
	if goal == "" {
		return false
	}
	actionHints := []string{
		"create", "write", "generate", "save", "draft", "produce", "build",
	}
	hasAction := false
	for _, hint := range actionHints {
		if strings.Contains(goal, hint) {
			hasAction = true
			break
		}
	}
	if !hasAction {
		return false
	}
	artifactHints := []string{
		"file", "report", "document", "markdown", ".md", ".txt", ".json", ".csv", ".xml", ".html", "owasp",
	}
	for _, hint := range artifactHints {
		if strings.Contains(goal, hint) {
			return true
		}
	}
	return false
}

func indicatesMissingCreation(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	missingHints := []string{
		"not created", "didn't create", "didnt create", "doesn't exist", "doesnt exist",
		"no file", "missing file", "can't find", "cannot find", "i dont think", "i don't think",
	}
	for _, hint := range missingHints {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}

func isFailureSummaryIntent(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))
	if goal == "" {
		return false
	}
	hints := []string{
		"fail", "failed", "failure", "error", "didn't", "didnt", "couldn't", "couldnt",
		"why not", "what went wrong", "issue",
	}
	for _, hint := range hints {
		if strings.Contains(goal, hint) {
			return true
		}
	}
	return false
}

func (r *Runner) recoveryDirectiveForGoal(goal, mode string) string {
	if mode != "recover" && mode != "follow-up" {
		return ""
	}
	if !isWriteCreationIntent(goal) {
		return ""
	}
	obs, ok := r.latestObservation()
	if !ok || obs.ExitCode != 0 {
		return ""
	}
	command := strings.ToLower(strings.TrimSpace(obs.Command))
	switch command {
	case "list_dir", "ls", "read_file", "read":
		return "Recovery directive: previous step only inspected existing files and produced no write output. Choose a create/write action now (for example, write_file with the requested report filename). Do not repeat list/read checks."
	default:
		return ""
	}
}
