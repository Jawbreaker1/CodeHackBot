package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
	"golang.org/x/term"
)

func renderTUI(out io.Writer, runID string, snap tuiSnapshot, messages []string, input string, commandLogScroll *int, eventLimit int) {
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
	lines = append(lines, styleLine(bar, width, tuiStyleBar))
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
	}
	left = append(left, "")
	left = append(left, "Recent Events:")
	if len(snap.events) == 0 {
		left = append(left, "  (no events)")
	} else {
		start := 0
		if len(snap.events) > tuiRecentEventsMax {
			start = len(snap.events) - tuiRecentEventsMax
		}
		for _, event := range snap.events[start:] {
			left = append(left, fmt.Sprintf("  - %s | %s | worker=%s | task=%s", event.TS.Format("15:04:05"), event.Type, event.WorkerID, event.TaskID))
		}
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

func appendLogLines(lines []string, text string) []string {
	chunks := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := append([]string{}, lines...)
	for _, chunk := range chunks {
		cleaned := sanitizeLogLine(chunk)
		if cleaned == "" {
			continue
		}
		out = append(out, cleaned)
	}
	if len(out) > tuiCommandLogStoreLines {
		return out[len(out)-tuiCommandLogStoreLines:]
	}
	return out
}

func sanitizeLogLine(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if r < 32 && r != '\t' {
			continue
		}
		if r == '\t' {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func renderCommandLog(messages []string, wrapWidth int, scroll *int) []string {
	if wrapWidth < 24 {
		wrapWidth = 24
	}
	rendered := make([]string, 0, len(messages)*2)
	for _, msg := range messages {
		parts := wrapText(msg, wrapWidth)
		if len(parts) == 0 {
			continue
		}
		for i, part := range parts {
			if i == 0 {
				rendered = append(rendered, "  - "+part)
			} else {
				rendered = append(rendered, "    "+part)
			}
		}
	}
	if len(rendered) == 0 {
		rendered = append(rendered, "  (empty)")
	}
	maxScroll := 0
	if len(rendered) > tuiCommandLogViewLines {
		maxScroll = len(rendered) - tuiCommandLogViewLines
	}
	offset := 0
	if scroll != nil {
		offset = *scroll
	}
	if offset < 0 {
		offset = 0
	}
	if offset > maxScroll {
		offset = maxScroll
	}
	if scroll != nil {
		*scroll = offset
	}
	end := len(rendered) - offset
	start := end - tuiCommandLogViewLines
	if start < 0 {
		start = 0
	}
	view := append([]string{}, rendered[start:end]...)
	if start > 0 {
		view = append([]string{fmt.Sprintf("  ... %d older line(s) (up/down to scroll)", start)}, view...)
	}
	if end < len(rendered) {
		view = append(view, fmt.Sprintf("  ... %d newer line(s)", len(rendered)-end))
	}
	return view
}

func wrapText(text string, width int) []string {
	cleaned := sanitizeLogLine(text)
	if cleaned == "" {
		return nil
	}
	if width <= 0 {
		return []string{cleaned}
	}
	words := strings.Fields(cleaned)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 2)
	current := words[0]
	for _, word := range words[1:] {
		if len([]rune(current))+1+len([]rune(word)) <= width {
			current += " " + word
			continue
		}
		lines = append(lines, current)
		if len([]rune(word)) > width {
			runes := []rune(word)
			for len(runes) > width {
				lines = append(lines, string(runes[:width]))
				runes = runes[width:]
			}
			current = string(runes)
			continue
		}
		current = word
	}
	if strings.TrimSpace(current) != "" {
		lines = append(lines, current)
	}
	return lines
}

func clampTUIBodyLines(lines []string, maxBody int) []string {
	if maxBody <= 0 {
		return nil
	}
	if len(lines) <= maxBody {
		return lines
	}
	const headKeep = 10
	head := headKeep
	if head > maxBody {
		head = maxBody
	}
	tail := maxBody - head
	if tail <= 0 {
		return append([]string{}, lines[:head]...)
	}
	out := make([]string, 0, maxBody)
	out = append(out, lines[:head]...)
	out = append(out, lines[len(lines)-tail:]...)
	return out
}

type tuiPaneLayout struct {
	enabled    bool
	leftWidth  int
	rightWidth int
	gap        int
}

func computePaneLayout(width int) tuiPaneLayout {
	contentWidth := width - 1
	if contentWidth < 100 {
		return tuiPaneLayout{}
	}
	layout := tuiPaneLayout{
		enabled: true,
		gap:     2,
	}
	layout.rightWidth = contentWidth / 3
	if layout.rightWidth < 38 {
		layout.rightWidth = 38
	}
	if layout.rightWidth > 68 {
		layout.rightWidth = 68
	}
	layout.leftWidth = contentWidth - layout.gap - layout.rightWidth
	if layout.leftWidth < 52 {
		layout.leftWidth = 52
		layout.rightWidth = contentWidth - layout.gap - layout.leftWidth
	}
	if layout.rightWidth < 32 {
		return tuiPaneLayout{}
	}
	return layout
}

func renderTwoPane(left []string, right []string, width int, layout tuiPaneLayout) []string {
	if !layout.enabled || len(right) == 0 {
		out := make([]string, 0, len(left)+len(right)+1)
		for _, line := range left {
			out = append(out, fitLine(line, width))
		}
		if len(right) > 0 {
			out = append(out, fitLine("", width))
			for _, line := range right {
				out = append(out, fitLine(line, width))
			}
		}
		return out
	}
	rows := len(left)
	if rows == 0 {
		rows = len(right)
	}
	if rows == 0 {
		return nil
	}
	if len(right) > rows {
		right = truncatePaneLines(right, rows, "worker lines")
	}
	gap := strings.Repeat(" ", layout.gap)
	out := make([]string, 0, rows)
	for i := 0; i < rows; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, fitColumn(l, layout.leftWidth)+gap+fitColumn(r, layout.rightWidth))
	}
	return out
}

func truncatePaneLines(lines []string, maxRows int, label string) []string {
	if maxRows <= 0 || len(lines) <= maxRows {
		return lines
	}
	trimmed := append([]string{}, lines[:maxRows]...)
	hidden := len(lines) - maxRows + 1
	if hidden < 1 {
		hidden = 1
	}
	suffix := "more"
	if strings.TrimSpace(label) != "" {
		suffix = label
	}
	trimmed[maxRows-1] = fmt.Sprintf("... %d %s", hidden, suffix)
	return trimmed
}

func fitColumn(line string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(line)
	if len(runes) > width {
		if width > 3 {
			return string(runes[:width-3]) + "..."
		}
		return string(runes[:width])
	}
	if len(runes) < width {
		return string(runes) + strings.Repeat(" ", width-len(runes))
	}
	return string(runes)
}

func renderWorkerBoxes(workers []orchestrator.WorkerStatus, debug map[string]tuiWorkerDebug, width int) []string {
	if len(workers) == 0 {
		return renderBox("Worker", []string{"no workers running"}, width)
	}
	sortedWorkers := sortWorkersForDebug(workers, debug)
	out := make([]string, 0, len(sortedWorkers)*8)
	for idx, worker := range sortedWorkers {
		task := strings.TrimSpace(worker.CurrentTask)
		if task == "" {
			task = "-"
		}
		body := []string{
			fmt.Sprintf("state=%s seq=%d task=%s", strings.TrimSpace(worker.State), worker.LastSeq, task),
		}
		if snapshot, ok := debug[worker.WorkerID]; ok {
			body = append(body, fmt.Sprintf("last=%s type=%s", snapshot.TS.Format("15:04:05"), snapshot.EventType))
			if snapshot.Step > 0 || snapshot.ToolCalls > 0 {
				body = append(body, fmt.Sprintf("step=%d tool_calls=%d", snapshot.Step, snapshot.ToolCalls))
			}
			if msg := strings.TrimSpace(snapshot.Message); msg != "" {
				body = append(body, "msg: "+msg)
			}
			if cmd := strings.TrimSpace(snapshot.Command); cmd != "" {
				args := strings.Join(snapshot.Args, " ")
				if args != "" {
					body = append(body, "cmd: "+cmd+" "+args)
				} else {
					body = append(body, "cmd: "+cmd)
				}
			}
			if reason := strings.TrimSpace(snapshot.Reason); reason != "" {
				body = append(body, "reason: "+reason)
			}
			if errText := strings.TrimSpace(snapshot.Error); errText != "" {
				body = append(body, "error: "+errText)
			}
			if path := strings.TrimSpace(snapshot.LogPath); path != "" {
				body = append(body, "log: "+path)
			}
		} else {
			body = append(body, "last: no worker output yet")
		}
		title := "Worker " + worker.WorkerID
		out = append(out, renderBox(title, body, width)...)
		if idx < len(sortedWorkers)-1 {
			out = append(out, fitLine("", width))
		}
	}
	return out
}

func sortWorkersForDebug(workers []orchestrator.WorkerStatus, debug map[string]tuiWorkerDebug) []orchestrator.WorkerStatus {
	out := append([]orchestrator.WorkerStatus{}, workers...)
	stateRank := func(state string) int {
		switch strings.ToLower(strings.TrimSpace(state)) {
		case "active":
			return 0
		case "seen":
			return 1
		case "stopped":
			return 2
		default:
			return 3
		}
	}
	lastTS := func(worker orchestrator.WorkerStatus) time.Time {
		ts := worker.LastEvent
		if snapshot, ok := debug[worker.WorkerID]; ok && snapshot.TS.After(ts) {
			ts = snapshot.TS
		}
		return ts
	}
	sort.SliceStable(out, func(i, j int) bool {
		leftRank := stateRank(out[i].State)
		rightRank := stateRank(out[j].State)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftTS := lastTS(out[i])
		rightTS := lastTS(out[j])
		if !leftTS.Equal(rightTS) {
			return leftTS.After(rightTS)
		}
		return out[i].WorkerID < out[j].WorkerID
	})
	return out
}

func renderBox(title string, body []string, width int) []string {
	inner := width - 5
	if inner < 12 {
		inner = 12
	}
	clip := func(text string) string {
		runes := []rune(strings.TrimSpace(text))
		if len(runes) > inner {
			if inner > 3 {
				return string(runes[:inner-3]) + "..."
			}
			return string(runes[:inner])
		}
		return string(runes)
	}
	pad := func(text string) string {
		runes := []rune(text)
		if len(runes) < inner {
			return text + strings.Repeat(" ", inner-len(runes))
		}
		return text
	}

	top := "+" + strings.Repeat("-", inner+2) + "+"
	lines := []string{
		fitLine(top, width),
		fitLine("| "+pad(clip(title))+" |", width),
		fitLine("+"+strings.Repeat("-", inner+2)+"+", width),
	}
	if len(body) == 0 {
		lines = append(lines, fitLine("| "+pad(" ")+" |", width))
	} else {
		for _, raw := range body {
			text := clip(raw)
			lines = append(lines, fitLine("| "+pad(text)+" |", width))
		}
	}
	lines = append(lines, fitLine(top, width))
	return lines
}

func failureHint(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case orchestrator.WorkerFailureAssistTimeout:
		return "LLM call timed out before task budget completed."
	case orchestrator.WorkerFailureAssistUnavailable:
		return "LLM was unreachable or returned an invalid response."
	case orchestrator.WorkerFailureCommandTimeout:
		return "Tool command timed out."
	case "execution_timeout":
		return "Task runtime budget exceeded."
	default:
		return ""
	}
}
