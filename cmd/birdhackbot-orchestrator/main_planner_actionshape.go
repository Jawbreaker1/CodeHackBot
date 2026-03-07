package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

var plannerAnchorTokenPattern = regexp.MustCompile(`(?i)(?:[a-z0-9_\-./]+(?:\.[a-z0-9_\-./]+)+|(?:\d{1,3}\.){3}\d{1,3}(?:/\d{1,2})?)`)

func normalizePlannerCommandExecutability(tasks []orchestrator.TaskSpec) ([]orchestrator.TaskSpec, string) {
	if len(tasks) == 0 {
		return tasks, ""
	}
	updated := append([]orchestrator.TaskSpec{}, tasks...)
	promoted := 0
	for idx, task := range updated {
		if !plannerCommandNeedsAssistExpansion(task, updated) {
			continue
		}
		updated[idx] = promotePlannerCommandTaskToAssist(task, updated)
		promoted++
	}
	if promoted == 0 {
		return tasks, ""
	}
	return updated, fmt.Sprintf("planner executability normalization: promoted %d under-specified direct command tasks to assist mode for LLM expansion", promoted)
}

func plannerCommandNeedsAssistExpansion(task orchestrator.TaskSpec, tasks []orchestrator.TaskSpec) bool {
	if !strings.EqualFold(strings.TrimSpace(task.Action.Type), "command") {
		return false
	}
	actionText := strings.ToLower(strings.TrimSpace(strings.Join(append([]string{task.Action.Command}, task.Action.Args...), " ")))
	if actionText == "" {
		return false
	}
	anchors := plannerTaskContractAnchors(task, tasks)
	if len(anchors) == 0 {
		return false
	}
	for _, anchor := range anchors {
		if strings.Contains(actionText, anchor) {
			return false
		}
	}
	return true
}

func plannerTaskContractAnchors(task orchestrator.TaskSpec, tasks []orchestrator.TaskSpec) []string {
	anchors := make([]string, 0, 12)
	for _, target := range task.Targets {
		for _, anchor := range plannerAnchorsFromText(target) {
			anchors = appendUniqueAnchor(anchors, anchor)
		}
	}
	for _, text := range []string{task.Title, task.Goal} {
		for _, anchor := range plannerAnchorsFromText(text) {
			anchors = appendUniqueAnchor(anchors, anchor)
		}
	}
	depByID := make(map[string]orchestrator.TaskSpec, len(tasks))
	for _, candidate := range tasks {
		depByID[strings.TrimSpace(candidate.TaskID)] = candidate
	}
	for _, depID := range task.DependsOn {
		dep := depByID[strings.TrimSpace(depID)]
		for _, path := range dep.ExpectedArtifacts {
			if anchor := normalizePlannerAnchor(filepath.Base(strings.TrimSpace(path))); anchor != "" {
				anchors = appendUniqueAnchor(anchors, anchor)
			}
		}
	}
	return anchors
}

func plannerAnchorsFromText(text string) []string {
	matches := plannerAnchorTokenPattern.FindAllString(strings.ToLower(text), -1)
	if len(matches) == 0 {
		return nil
	}
	anchors := make([]string, 0, len(matches))
	for _, match := range matches {
		if anchor := normalizePlannerAnchor(match); anchor != "" {
			anchors = appendUniqueAnchor(anchors, anchor)
		}
	}
	return anchors
}

func normalizePlannerAnchor(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	trimmed = strings.Trim(trimmed, `"'()[]{}<>,;`)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "/") {
		trimmed = filepath.Base(trimmed)
	}
	if trimmed == "." || trimmed == ".." {
		return ""
	}
	return trimmed
}

func appendUniqueAnchor(items []string, candidate string) []string {
	candidate = normalizePlannerAnchor(candidate)
	if candidate == "" {
		return items
	}
	for _, item := range items {
		if item == candidate {
			return items
		}
	}
	return append(items, candidate)
}

func promotePlannerCommandTaskToAssist(task orchestrator.TaskSpec, tasks []orchestrator.TaskSpec) orchestrator.TaskSpec {
	prompt := strings.TrimSpace(task.Action.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(task.Goal)
	}
	if prompt == "" {
		prompt = strings.TrimSpace(task.Title)
	}
	commandHint := strings.TrimSpace(strings.Join(append([]string{strings.TrimSpace(task.Action.Command)}, task.Action.Args...), " "))
	lines := []string{prompt}
	if commandHint != "" {
		lines = append(lines, fmt.Sprintf("Previous command hint was %q; treat it as optional and adapt as needed.", commandHint))
	}
	if anchors := plannerTaskContractAnchors(task, tasks); len(anchors) > 0 {
		lines = append(lines, "Task contract anchors: "+strings.Join(anchors, ", "))
	}
	if len(task.DoneWhen) > 0 {
		lines = append(lines, "Done when: "+strings.Join(task.DoneWhen, "; "))
	}
	if len(task.FailWhen) > 0 {
		lines = append(lines, "Fail when: "+strings.Join(task.FailWhen, "; "))
	}
	lines = append(lines, "Use concrete in-scope targets/artifacts from the task contract or dependencies. Emit a bounded executable step, not a bare tool label.")
	task.Action = orchestrator.TaskAction{
		Type:           "assist",
		Prompt:         strings.TrimSpace(strings.Join(lines, "\n")),
		WorkingDir:     task.Action.WorkingDir,
		TimeoutSeconds: task.Action.TimeoutSeconds,
	}
	return task
}
