package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
	"golang.org/x/term"
)

func renderTUI(out io.Writer, runID string, snap tuiSnapshot, messages []string, input string, commandLogScroll *int, eventScroll *int, eventLimit int) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 40
	}

	lines := make([]string, 0, height)
	pane := computePaneLayout(width)
	runPhase := orchestrator.NormalizeRunPhase(snap.plan.Metadata.RunPhase)
	if runPhase == "" {
		runPhase = "-"
	}
	bar := fmt.Sprintf(" Orchestrator TUI | run:%s | state:%s | phase:%s | workers:%d | queued:%d | running:%d | approvals:%d | events:%d ",
		runID,
		snap.status.State,
		runPhase,
		snap.status.ActiveWorkers,
		snap.status.QueuedTasks,
		snap.status.RunningTasks,
		len(snap.approvals),
		eventLimit,
	)
	lines = append(lines, styleLine(bar, width, tuiBarStyleForRunState(snap.status.State)))
	lines = append(lines, fitLine(fmt.Sprintf("Updated: %s", snap.updatedAt.Format("2006-01-02 15:04:05 MST")), width))
	left := make([]string, 0, height)
	right := make([]string, 0, height/2)

	left = append(left, "")
	left = append(left, "Plan:")
	goal := strings.TrimSpace(snap.plan.Metadata.Goal)
	if goal == "" {
		goal = "(no goal metadata)"
	}
	left = append(left, "  - Goal: "+goal)
	left = append(left, "  - Phase: "+runPhase)
	left = append(left, fmt.Sprintf("  - Tasks: %d | Success criteria: %d | Stop criteria: %d", len(snap.plan.Tasks), len(snap.plan.SuccessCriteria), len(snap.plan.StopCriteria)))
	reportPath := strings.TrimSpace(snap.reportPath)
	if reportPath == "" {
		reportPath = "(pending)"
	}
	reportStatus := "pending"
	if snap.reportReady {
		reportStatus = "ready"
	}
	left = append(left, fmt.Sprintf("  - Report (%s): %s", reportStatus, reportPath))
	left = append(left, "")
	left = append(left, "Execution:")
	stateByTaskID := map[string]tuiTaskRow{}
	counts := map[string]int{}
	for _, task := range snap.tasks {
		stateByTaskID[task.TaskID] = task
		counts[task.State]++
	}
	remainingSteps := counts["queued"] + counts["leased"] + counts["awaiting_approval"] + counts["blocked"]
	left = append(left, fmt.Sprintf("  - Completed: %d/%d | Running: %d | Remaining: %d | Failed: %d", counts["completed"], len(snap.plan.Tasks), counts["running"], remainingSteps, counts["failed"]))
	activeRows := activeTaskRows(snap.tasks)
	if len(activeRows) == 0 {
		left = append(left, "  - Current: (no active task)")
	} else {
		for i, task := range activeRows {
			if i >= 2 {
				left = append(left, fmt.Sprintf("  ... %d more active tasks", len(activeRows)-i))
				break
			}
			progress := formatProgressSummary(snap.progress[task.TaskID])
			if progress == "" {
				progress = "started; waiting for progress update"
			}
			left = append(left, fmt.Sprintf("  - Current: %s | %s", task.TaskID, progress))
		}
	}
	left = append(left, "  - Next planned steps:")
	nextCount := 0
	for _, task := range snap.plan.Tasks {
		row, ok := stateByTaskID[task.TaskID]
		if !ok {
			continue
		}
		if row.State == "completed" || row.State == "failed" || row.State == "canceled" {
			continue
		}
		left = append(left, fmt.Sprintf("    %d) %s [%s]", nextCount+1, task.Title, row.State))
		nextCount++
		if nextCount >= 4 {
			break
		}
	}
	if nextCount == 0 {
		left = append(left, "    (all planned steps complete or terminal)")
	}
	left = append(left, "")
	left = append(left, "Plan Steps:")
	if len(snap.plan.Tasks) == 0 {
		left = append(left, "  (no planned tasks)")
	} else {
		for i, task := range snap.plan.Tasks {
			row, ok := stateByTaskID[task.TaskID]
			state := "queued"
			if ok {
				state = row.State
			}
			left = append(left, fmt.Sprintf("  %d. %s %s", i+1, stepMarker(state), task.Title))
			if state == "running" || state == "leased" || state == "awaiting_approval" {
				if progress := formatProgressSummary(snap.progress[task.TaskID]); progress != "" {
					left = append(left, "     now: "+progress)
				}
			}
			if i >= 7 && len(snap.plan.Tasks) > 8 {
				left = append(left, fmt.Sprintf("  ... %d more planned steps", len(snap.plan.Tasks)-i-1))
				break
			}
		}
	}
	right = append(right, "")
	right = append(right, "Worker Debug:")
	workerBoxWidth := width
	if pane.enabled {
		workerBoxWidth = pane.rightWidth
	}
	right = append(right, renderWorkerBoxes(snap.workers, snap.workerDebug, workerBoxWidth)...)

	left = append(left, "")
	left = append(left, "Task Board:")
	if len(snap.tasks) == 0 {
		left = append(left, "  (no tasks)")
	} else {
		for i, task := range snap.tasks {
			if i >= 12 {
				left = append(left, fmt.Sprintf("  ... %d more", len(snap.tasks)-i))
				break
			}
			dynamic := ""
			if task.IsDynamic {
				dynamic = " (dynamic)"
			}
			worker := task.WorkerID
			if strings.TrimSpace(worker) == "" {
				worker = "-"
			}
			strategy := task.Strategy
			if strings.TrimSpace(strategy) == "" {
				strategy = "-"
			}
			left = append(left, fmt.Sprintf("  - %s [%s] worker=%s strategy=%s%s", task.TaskID, task.State, worker, strategy, dynamic))
		}
	}
	left = append(left, "")
	left = append(left, "Pending Approvals:")
	if len(snap.approvals) == 0 {
		left = append(left, "  (none)")
	} else {
		for _, approval := range snap.approvals {
			left = append(left, fmt.Sprintf("  - %s | task=%s | tier=%s | %s", approval.ApprovalID, approval.TaskID, approval.RiskTier, approval.Reason))
		}
	}
	left = append(left, "")
	left = append(left, "Last Failure:")
	if snap.lastFailure == nil {
		left = append(left, "  (none)")
	} else {
		taskID := snap.lastFailure.TaskID
		if strings.TrimSpace(taskID) == "" {
			taskID = "-"
		}
		reason := snap.lastFailure.Reason
		if strings.TrimSpace(reason) == "" {
			reason = "task_failed"
		}
		left = append(left, fmt.Sprintf("  - %s | task=%s | worker=%s | reason=%s", snap.lastFailure.TS.Format("15:04:05"), taskID, snap.lastFailure.Worker, reason))
		if hint := failureHint(reason); hint != "" {
			left = append(left, "    hint: "+hint)
		}
		if strings.TrimSpace(snap.lastFailure.Error) != "" {
			left = append(left, "    error: "+snap.lastFailure.Error)
		}
		if strings.TrimSpace(snap.lastFailure.LogPath) != "" {
			left = append(left, "    log: "+snap.lastFailure.LogPath)
		}
		for _, detail := range snap.lastFailure.Details {
			left = append(left, "    detail: "+detail)
		}
	}
	left = append(left, "")
	left = append(left, "Recent Events:")
	for _, line := range renderRecentEvents(snap.events, eventScroll) {
		left = append(left, line)
	}
	left = append(left, "")
	left = append(left, "Command Log:")
	logWrapWidth := width - 6
	if pane.enabled {
		logWrapWidth = pane.leftWidth - 6
	}
	for _, line := range renderCommandLog(messages, logWrapWidth, commandLogScroll) {
		left = append(left, line)
	}
	lines = append(lines, renderTwoPane(left, right, width, pane)...)

	maxBody := height - 2
	if maxBody < 1 {
		maxBody = 1
	}
	lines = clampTUIBodyLines(lines, maxBody)
	lines = colorizeTUIRows(lines)

	_, _ = fmt.Fprint(out, "\x1b[H\x1b[2J")
	for _, line := range lines {
		_, _ = fmt.Fprintf(out, "%s\r\n", line)
	}
	for i := len(lines); i < maxBody; i++ {
		_, _ = fmt.Fprint(out, "\r\n")
	}
	promptLine := styleLine(" orchestrator> "+input, width, tuiStylePrompt)
	_, _ = fmt.Fprint(out, promptLine+tuiStyleReset)
}

func styleLine(line string, width int, style string) string {
	return style + fitLine(line, width) + tuiStyleReset
}

func fitLine(line string, width int) string {
	if width <= 1 {
		return line
	}
	runes := []rune(line)
	max := width - 1
	if len(runes) > max {
		if max > 3 {
			return string(runes[:max-3]) + "..."
		}
		return string(runes[:max])
	}
	if len(runes) < max {
		return string(runes) + strings.Repeat(" ", max-len(runes))
	}
	return string(runes)
}
