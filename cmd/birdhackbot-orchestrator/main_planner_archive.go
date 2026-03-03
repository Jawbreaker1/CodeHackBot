package main

import (
	"fmt"
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
	if len(archiveCrackTaskIDs) == 0 {
		return tasks, ""
	}
	updated := append([]orchestrator.TaskSpec{}, tasks...)
	changed := false
	updatedCount := 0
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
				updatedCount++
			}
			continue
		}
		if !taskLooksLikeArchiveExtraction(task) {
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
			updated[i] = task
			updatedCount++
		}
	}
	if !changed {
		return tasks, ""
	}
	return updated, fmt.Sprintf("archive workflow normalization: tightened %d task dependency edges so hash extraction precedes cracking and extraction waits for all cracking strategies", updatedCount)
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
