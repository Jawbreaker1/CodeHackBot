package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func normalizeArchiveWorkflowTaskDependencies(goal string, tasks []orchestrator.TaskSpec) ([]orchestrator.TaskSpec, string) {
	if len(tasks) == 0 || !goalLikelyArchiveWorkflow(goal, tasks) {
		return tasks, ""
	}
	archiveCrackTaskIDs := make([]string, 0, len(tasks))
	archiveHashTaskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if taskLooksLikeArchiveHashMaterial(task) {
			archiveHashTaskIDs = append(archiveHashTaskIDs, task.TaskID)
		}
		if taskLooksLikeArchiveCrackStrategy(task) {
			archiveCrackTaskIDs = append(archiveCrackTaskIDs, task.TaskID)
		}
	}
	updated := append([]orchestrator.TaskSpec{}, tasks...)
	changed := false
	dependencyUpdatedCount := 0
	assistPromotedCount := 0
	for i, task := range updated {
		if taskLooksLikeArchiveCrackStrategy(task) && len(archiveHashTaskIDs) > 0 {
			deps := append([]string{}, task.DependsOn...)
			taskChanged := false
			for _, dep := range archiveHashTaskIDs {
				if dep == task.TaskID {
					continue
				}
				if !containsString(deps, dep) {
					deps = append(deps, dep)
					changed = true
					taskChanged = true
				}
			}
			if taskChanged {
				task.DependsOn = compactStrings(deps)
				updated[i] = task
				dependencyUpdatedCount++
			}
			continue
		}
		if !taskLooksLikeArchiveExtraction(task) {
			if shouldPromoteArchiveTaskToAssist(task) {
				promoted := promoteArchiveTaskToAssist(task)
				updated[i] = promoted
				changed = true
				assistPromotedCount++
			}
			continue
		}
		deps := append([]string{}, task.DependsOn...)
		taskChanged := false
		for _, dep := range archiveCrackTaskIDs {
			if dep == task.TaskID {
				continue
			}
			if !containsString(deps, dep) {
				deps = append(deps, dep)
				changed = true
				taskChanged = true
			}
		}
		if taskChanged {
			task.DependsOn = compactStrings(deps)
			changed = true
			dependencyUpdatedCount++
		}
		if shouldPromoteArchiveTaskToAssist(task) {
			task = promoteArchiveTaskToAssist(task)
			assistPromotedCount++
			changed = true
		}
		updated[i] = task
	}
	if !changed {
		return tasks, ""
	}
	notes := make([]string, 0, 2)
	if dependencyUpdatedCount > 0 {
		notes = append(notes, fmt.Sprintf("archive workflow normalization: tightened %d task dependency edges so hash extraction precedes cracking and extraction waits for all cracking strategies", dependencyUpdatedCount))
	}
	if assistPromotedCount > 0 {
		notes = append(notes, fmt.Sprintf("archive workflow normalization: promoted %d under-specified extraction/validation command tasks to assist mode for closed-loop recovery", assistPromotedCount))
	}
	return updated, strings.Join(notes, " | ")
}

func goalLikelyArchiveWorkflow(goal string, tasks []orchestrator.TaskSpec) bool {
	text := strings.ToLower(strings.TrimSpace(goal))
	if containsAny(text, "zip", "archive", "password", "crack", "decrypt") {
		return true
	}
	for _, task := range tasks {
		if taskLooksLikeArchiveCrackStrategy(task) || taskLooksLikeArchiveExtraction(task) {
			return true
		}
	}
	return false
}

func taskLooksLikeArchiveCrackStrategy(task orchestrator.TaskSpec) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		task.Action.Command,
		strings.Join(task.Action.Args, " "),
	}, " ")))
	if text == "" {
		return false
	}
	if containsAny(text, "report", "summary", "extract archive", "extract contents", "unzip -p") {
		return false
	}
	if containsAny(text, "validate", "validation", "proof-of-access", "proof of access", "access_validation") {
		return false
	}
	if taskLooksLikeArchiveHashMaterial(task) {
		return false
	}
	return containsAny(text, "john", "fcrackzip", "wordlist", "rules crack", "fallback crack", "password recovery", "crack")
}

func taskLooksLikeArchiveHashMaterial(task orchestrator.TaskSpec) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		task.Action.Command,
		strings.Join(task.Action.Args, " "),
	}, " ")))
	if text == "" {
		return false
	}
	return containsAny(text, "zip2john", "hash material", "generate hash", "extract hash")
}

func taskLooksLikeArchiveExtraction(task orchestrator.TaskSpec) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		task.Action.Command,
		strings.Join(task.Action.Args, " "),
	}, " ")))
	if text == "" {
		return false
	}
	return containsAny(text, "extract archive", "extract contents", "unzip -p", "proof-of-access")
}

func taskLooksLikeArchiveValidation(task orchestrator.TaskSpec) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		task.Action.Command,
		strings.Join(task.Action.Args, " "),
	}, " ")))
	if text == "" {
		return false
	}
	return containsAny(text, "validate password", "proof", "proof-of-access", "access_validation", "extract")
}

func shouldPromoteArchiveTaskToAssist(task orchestrator.TaskSpec) bool {
	if !taskLikelyArchiveWorkflowTask(task) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(task.Action.Type), "command") {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(task.Action.Command)))
	if base == "" {
		return false
	}
	switch base {
	case "john", "fcrackzip", "unzip", "zip", "cat", "grep", "awk", "sed", "7z", "openssl", "gpg", "test", "find", "ls", "stat":
		return true
	default:
		return false
	}
}

func taskLikelyArchiveWorkflowTask(task orchestrator.TaskSpec) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		task.Action.Command,
		strings.Join(task.Action.Args, " "),
		strings.Join(task.ExpectedArtifacts, " "),
	}, " ")))
	if text == "" {
		return false
	}
	return containsAny(text, "archive", "zip", "password", "decrypt", "crack", "extract", "metadata")
}

func hasNonFlagArg(args []string) bool {
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" || strings.HasPrefix(trimmed, "-") {
			continue
		}
		return true
	}
	return false
}

func promoteArchiveTaskToAssist(task orchestrator.TaskSpec) orchestrator.TaskSpec {
	prompt := strings.TrimSpace(task.Action.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(task.Goal)
	}
	commandHint := strings.TrimSpace(task.Action.Command)
	hintLine := ""
	if commandHint != "" {
		hintLine = fmt.Sprintf("Previous command hint was %q; treat it as optional and adapt as needed.", commandHint)
	}
	extra := "Use dependency artifacts first, run a concrete bounded command, and self-correct on errors. If password proof is missing, pivot to bounded recovery instead of failing immediately."
	if hintLine != "" {
		prompt = strings.TrimSpace(strings.Join([]string{prompt, hintLine, extra}, "\n"))
	} else {
		prompt = strings.TrimSpace(strings.Join([]string{prompt, extra}, "\n"))
	}
	task.Action.Type = "assist"
	task.Action.Prompt = prompt
	task.Action.Command = ""
	task.Action.Args = nil
	return task
}

func containsAny(text string, tokens ...string) bool {
	for _, token := range tokens {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(token))) {
			return true
		}
	}
	return false
}

func containsString(items []string, candidate string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(candidate) {
			return true
		}
	}
	return false
}
