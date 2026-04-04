package interactivecli

import (
	"fmt"
	"strings"
)

const (
	leftPaneWidth  = 78
	rightPaneWidth = 38
)

func renderDashboard(view ViewState, stream []string, prompt string) string {
	leftHeader := padRight(" Stream ", leftPaneWidth)
	rightHeader := padRight(" Status ", rightPaneWidth)

	leftLines := renderStreamPane(stream, leftPaneWidth)
	rightLines := renderStatusPane(view, rightPaneWidth)
	bodyHeight := max(len(leftLines), len(rightLines))
	leftLines = padLines(leftLines, bodyHeight)
	rightLines = padLines(rightLines, bodyHeight)

	var lines []string
	lines = append(lines, joinPanes(leftHeader, rightHeader))
	lines = append(lines, joinPanes(strings.Repeat("-", leftPaneWidth), strings.Repeat("-", rightPaneWidth)))
	for i := 0; i < bodyHeight; i++ {
		lines = append(lines, joinPanes(leftLines[i], rightLines[i]))
	}
	lines = append(lines, strings.Repeat("=", leftPaneWidth+rightPaneWidth+3))
	lines = append(lines, " Input ")
	lines = append(lines, padRight(prompt, leftPaneWidth+rightPaneWidth+3))
	lines = append(lines, padRight("Commands: /status /step /plan /lastlog /stats /packet /help", leftPaneWidth+rightPaneWidth+3))
	return strings.Join(lines, "\n")
}

func renderStreamPane(stream []string, width int) []string {
	if len(stream) == 0 {
		return []string{padRight("(no activity yet)", width)}
	}
	var lines []string
	for _, item := range stream {
		for _, line := range wrapText(item, width) {
			lines = append(lines, padRight(line, width))
		}
	}
	return lines
}

func renderStatusPane(view ViewState, width int) []string {
	var lines []string
	lines = append(lines, sectionLines("Goal", firstNonEmpty(view.WorkerGoal, view.Goal), width)...)
	lines = append(lines, sectionLines("Plan", renderPlanSummary(view), width)...)
	lines = append(lines, sectionLines("Step", renderStepSummary(view), width)...)
	lines = append(lines, sectionLines("Latest", renderLatestSummary(view), width)...)
	lines = append(lines, sectionLines("Runtime", renderRuntimeSummary(view), width)...)
	return lines
}

func renderPlanSummary(view ViewState) string {
	parts := []string{
		"summary: " + blankOrNone(view.PlanSummary),
	}
	if len(view.PlanSteps) == 0 {
		parts = append(parts, "steps: (none)")
		return strings.Join(parts, "\n")
	}
	parts = append(parts, "steps:")
	for _, step := range view.PlanSteps {
		marker := "-"
		switch step.State {
		case StepVisualDone:
			marker = "x"
		case StepVisualInProgress:
			marker = ">"
		case StepVisualBlocked:
			marker = "!"
		}
		parts = append(parts, fmt.Sprintf("%s %s", marker, step.Label))
	}
	return strings.Join(parts, "\n")
}

func renderStepSummary(view ViewState) string {
	parts := []string{
		"objective: " + blankOrNone(view.CurrentObjective),
		"state: " + blankOrNone(string(view.CurrentStepState)),
		"active: " + blankOrNone(view.ActiveStep),
		"step_eval: " + blankOrNone(string(view.LatestStepEval)),
	}
	if strings.TrimSpace(view.LatestStepSummary) != "" {
		parts = append(parts, "summary: "+view.LatestStepSummary)
	}
	if review := blankOrNone(string(view.LatestActionReview)); review != "(none)" && review != "unknown" {
		parts = append(parts, "action_review: "+review)
	}
	return strings.Join(parts, "\n")
}

func renderLatestSummary(view ViewState) string {
	parts := []string{
		"command: " + blankOrNone(view.LatestCommand),
		"result: " + blankOrNone(view.LatestResultSummary),
		"assessment: " + blankOrNone(view.LatestAssessment),
	}
	if strings.TrimSpace(view.LatestFailureClass) != "" {
		parts = append(parts, "failure: "+view.LatestFailureClass)
	}
	if len(view.LatestSignals) > 0 {
		parts = append(parts, "signals: "+strings.Join(view.LatestSignals, ", "))
	}
	if strings.TrimSpace(view.LatestLog) != "" {
		parts = append(parts, "log: "+view.LatestLog)
	}
	return strings.Join(parts, "\n")
}

func renderRuntimeSummary(view ViewState) string {
	return strings.Join([]string{
		"reporting: " + blankOrNone(view.ReportingRequirement),
		"session: " + blankOrNone(view.SessionStatus),
		"task: " + blankOrNone(view.TaskState),
		"target: " + blankOrNone(view.CurrentTarget),
		"missing: " + blankOrNone(view.MissingFact),
		"scope: " + blankOrNone(view.ScopeState),
		"approval: " + blankOrNone(view.ApprovalState),
		"model: " + blankOrNone(view.Model),
		"context: " + blankOrNone(view.ContextUsage),
		"cwd: " + blankOrNone(view.WorkingDir),
	}, "\n")
}

func sectionLines(title, content string, width int) []string {
	lines := []string{padRight(title, width)}
	for _, paragraph := range strings.Split(content, "\n") {
		for _, line := range wrapText(strings.TrimSpace(paragraph), width) {
			lines = append(lines, padRight(line, width))
		}
	}
	lines = append(lines, padRight("", width))
	return lines
}

func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{""}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) <= width {
			line += " " + word
			continue
		}
		lines = append(lines, line)
		line = word
	}
	lines = append(lines, line)
	return lines
}

func joinPanes(left, right string) string {
	return left + " | " + right
}

func padLines(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
