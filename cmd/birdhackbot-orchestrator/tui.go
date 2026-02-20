package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
	"golang.org/x/term"
)

const (
	tuiStylePrompt = "\x1b[48;5;236m\x1b[38;5;252m"
	tuiStyleBar    = "\x1b[48;5;238m\x1b[38;5;250m"
	tuiStyleReset  = "\x1b[0m"
)

type tuiCommand struct {
	name       string
	approval   string
	scope      string
	reason     string
	eventLimit int
}

func runTUI(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sessionsDir, runID string
	var refresh time.Duration
	var eventLimit int
	var exitOnDone bool
	fs.StringVar(&sessionsDir, "sessions-dir", "sessions", "sessions base directory")
	fs.StringVar(&runID, "run", "", "run id")
	fs.DurationVar(&refresh, "refresh", time.Second, "refresh interval")
	fs.IntVar(&eventLimit, "events", 12, "max recent events to display")
	fs.BoolVar(&exitOnDone, "exit-on-done", false, "exit automatically when run reaches completed/stopped")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(runID) == "" {
		fmt.Fprintln(stderr, "tui requires --run")
		return 2
	}
	if refresh <= 0 {
		refresh = time.Second
	}
	if eventLimit <= 0 {
		eventLimit = 12
	}

	if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(stderr, "tui requires an interactive terminal")
		return 2
	}

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(stderr, "tui failed to initialize terminal: %v\n", err)
		return 1
	}
	defer func() {
		_ = term.Restore(int(os.Stdin.Fd()), state)
		_, _ = fmt.Fprint(stdout, "\x1b[?1049l")
	}()
	_, _ = fmt.Fprint(stdout, "\x1b[?1049h\x1b[H\x1b[2J")

	manager := orchestrator.NewManager(sessionsDir)
	keyCh := make(chan byte, 128)
	errCh := make(chan error, 1)
	go readTUIKeys(keyCh, errCh)

	ticker := time.NewTicker(refresh)
	defer ticker.Stop()

	input := ""
	messages := []string{"TUI ready. Type 'help' for commands."}
	running := true
	for running {
		snapshot, snapErr := collectTUISnapshot(manager, runID, eventLimit)
		if snapErr != nil {
			messages = appendLogLine(messages, "snapshot error: "+snapErr.Error())
		}
		if exitOnDone && (snapshot.status.State == "completed" || snapshot.status.State == "stopped") {
			messages = appendLogLine(messages, fmt.Sprintf("run %s %s; exiting tui", runID, snapshot.status.State))
			renderTUI(stdout, runID, snapshot, messages, input, eventLimit)
			break
		}
		renderTUI(stdout, runID, snapshot, messages, input, eventLimit)

		select {
		case <-ticker.C:
			continue
		case readErr := <-errCh:
			if readErr != nil && readErr != io.EOF {
				messages = appendLogLine(messages, "input error: "+readErr.Error())
			}
			running = false
		case b := <-keyCh:
			switch b {
			case '\r', '\n':
				cmd, parseErr := parseTUICommand(input)
				input = ""
				if parseErr != nil {
					messages = appendLogLine(messages, "invalid command: "+parseErr.Error())
					continue
				}
				done, logLine := executeTUICommand(manager, runID, &eventLimit, cmd)
				if logLine != "" {
					messages = appendLogLine(messages, logLine)
				}
				if done {
					running = false
				}
			case 0x03:
				running = false
			case 0x7f, 0x08:
				if len(input) > 0 {
					input = input[:len(input)-1]
				}
			default:
				if b >= 32 && b <= 126 {
					input += string(b)
				}
			}
		}
	}
	return 0
}

func readTUIKeys(keyCh chan<- byte, errCh chan<- error) {
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			errCh <- err
			return
		}
		if n == 1 {
			keyCh <- buf[0]
		}
	}
}

type tuiSnapshot struct {
	status      orchestrator.RunStatus
	plan        orchestrator.RunPlan
	workers     []orchestrator.WorkerStatus
	workerDebug map[string]tuiWorkerDebug
	approvals   []orchestrator.PendingApprovalView
	tasks       []tuiTaskRow
	progress    map[string]tuiTaskProgress
	events      []orchestrator.EventEnvelope
	lastFailure *tuiFailure
	updatedAt   time.Time
}

type tuiTaskRow struct {
	TaskID    string
	Title     string
	Strategy  string
	State     string
	WorkerID  string
	Priority  int
	IsDynamic bool
}

type tuiFailure struct {
	TS      time.Time
	TaskID  string
	Worker  string
	Reason  string
	Error   string
	LogPath string
}

type tuiTaskProgress struct {
	TS        time.Time
	Step      int
	ToolCalls int
	Message   string
	Mode      string
}

type tuiWorkerDebug struct {
	WorkerID  string
	TaskID    string
	EventType string
	Message   string
	Command   string
	Args      []string
	LogPath   string
	Reason    string
	Error     string
	Step      int
	ToolCalls int
	TS        time.Time
}

func collectTUISnapshot(manager *orchestrator.Manager, runID string, eventLimit int) (tuiSnapshot, error) {
	var snap tuiSnapshot
	status, err := manager.Status(runID)
	if err != nil {
		return snap, err
	}
	plan, err := manager.LoadRunPlan(runID)
	if err != nil {
		return snap, err
	}
	workers, err := manager.Workers(runID)
	if err != nil {
		return snap, err
	}
	leases, err := manager.ReadLeases(runID)
	if err != nil {
		return snap, err
	}
	approvals, err := manager.PendingApprovals(runID)
	if err != nil {
		return snap, err
	}
	allEvents, err := manager.Events(runID, 0)
	if err != nil {
		return snap, err
	}
	events := allEvents
	if eventLimit > 0 && len(events) > eventLimit {
		events = append([]orchestrator.EventEnvelope{}, events[len(events)-eventLimit:]...)
	}
	snap.status = status
	snap.plan = plan
	snap.workers = hydrateWorkerTasks(workers, leases)
	snap.workerDebug = latestWorkerDebugByWorker(allEvents)
	snap.approvals = approvals
	snap.tasks = buildTaskRows(plan, leases)
	snap.progress = latestTaskProgressByTask(allEvents)
	snap.events = events
	snap.lastFailure = latestFailureFromEvents(allEvents)
	snap.updatedAt = time.Now().UTC()
	return snap, nil
}

func renderTUI(out io.Writer, runID string, snap tuiSnapshot, messages []string, input string, eventLimit int) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 40
	}

	lines := make([]string, 0, height)
	pane := computePaneLayout(width)
	bar := fmt.Sprintf(" Orchestrator TUI | run:%s | state:%s | workers:%d | queued:%d | running:%d | approvals:%d | events:%d ",
		runID,
		snap.status.State,
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
		for _, event := range snap.events {
			left = append(left, fmt.Sprintf("  - %s | %s | worker=%s | task=%s", event.TS.Format("15:04:05"), event.Type, event.WorkerID, event.TaskID))
		}
	}
	left = append(left, "")
	left = append(left, "Command Log:")
	for _, msg := range messages {
		left = append(left, "  - "+msg)
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

func appendLogLine(lines []string, line string) []string {
	out := append(lines, strings.TrimSpace(line))
	if len(out) > 8 {
		return out[len(out)-8:]
	}
	return out
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

func hydrateWorkerTasks(workers []orchestrator.WorkerStatus, leases []orchestrator.TaskLease) []orchestrator.WorkerStatus {
	if len(workers) == 0 {
		return workers
	}
	leaseTask := map[string]string{}
	for _, lease := range leases {
		if strings.TrimSpace(lease.WorkerID) == "" {
			continue
		}
		if lease.Status != orchestrator.LeaseStatusLeased && lease.Status != orchestrator.LeaseStatusRunning && lease.Status != orchestrator.LeaseStatusAwaitingApproval {
			continue
		}
		leaseTask[lease.WorkerID] = lease.TaskID
	}
	out := make([]orchestrator.WorkerStatus, 0, len(workers))
	for _, worker := range workers {
		if strings.TrimSpace(worker.CurrentTask) == "" {
			if taskID := strings.TrimSpace(leaseTask[worker.WorkerID]); taskID != "" {
				worker.CurrentTask = taskID
			}
		}
		out = append(out, worker)
	}
	return out
}

func buildTaskRows(plan orchestrator.RunPlan, leases []orchestrator.TaskLease) []tuiTaskRow {
	leaseByTask := map[string]orchestrator.TaskLease{}
	for _, lease := range leases {
		leaseByTask[lease.TaskID] = lease
	}
	rows := make([]tuiTaskRow, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		state := "queued"
		workerID := ""
		if lease, ok := leaseByTask[task.TaskID]; ok {
			state = lease.Status
			workerID = strings.TrimSpace(lease.WorkerID)
		}
		row := tuiTaskRow{
			TaskID:    task.TaskID,
			Title:     task.Title,
			Strategy:  task.Strategy,
			State:     normalizeDisplayState(state),
			WorkerID:  workerID,
			Priority:  task.Priority,
			IsDynamic: strings.HasPrefix(strings.ToLower(strings.TrimSpace(task.Strategy)), "adaptive_replan_"),
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left := taskStateOrder(rows[i].State)
		right := taskStateOrder(rows[j].State)
		if left != right {
			return left < right
		}
		if rows[i].Priority != rows[j].Priority {
			return rows[i].Priority > rows[j].Priority
		}
		return rows[i].TaskID < rows[j].TaskID
	})
	return rows
}

func normalizeDisplayState(state string) string {
	trimmed := strings.TrimSpace(strings.ToLower(state))
	if trimmed == "" {
		return "queued"
	}
	return trimmed
}

func stepMarker(state string) string {
	switch normalizeDisplayState(state) {
	case "completed":
		return "[x]"
	case "running", "leased", "awaiting_approval":
		return "[>]"
	case "failed":
		return "[!]"
	case "blocked":
		return "[-]"
	case "canceled":
		return "[~]"
	default:
		return "[ ]"
	}
}

func taskStateOrder(state string) int {
	switch normalizeDisplayState(state) {
	case "running":
		return 1
	case "awaiting_approval":
		return 2
	case "leased":
		return 3
	case "queued":
		return 4
	case "failed":
		return 5
	case "blocked":
		return 6
	case "completed":
		return 7
	case "canceled":
		return 8
	default:
		return 9
	}
}

func latestFailureFromEvents(events []orchestrator.EventEnvelope) *tuiFailure {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		payload := decodeEventPayload(event.Payload)
		switch event.Type {
		case orchestrator.EventTypeTaskFailed:
			reason := strings.TrimSpace(stringFromAny(payload["reason"]))
			if reason == "" {
				reason = "task_failed"
			}
			return &tuiFailure{
				TS:      event.TS,
				TaskID:  event.TaskID,
				Worker:  event.WorkerID,
				Reason:  reason,
				Error:   strings.TrimSpace(stringFromAny(payload["error"])),
				LogPath: strings.TrimSpace(stringFromAny(payload["log_path"])),
			}
		case orchestrator.EventTypeWorkerStopped:
			status := strings.ToLower(strings.TrimSpace(stringFromAny(payload["status"])))
			if status != "failed" {
				continue
			}
			return &tuiFailure{
				TS:      event.TS,
				TaskID:  event.TaskID,
				Worker:  event.WorkerID,
				Reason:  "worker_stopped_failed",
				Error:   strings.TrimSpace(stringFromAny(payload["error"])),
				LogPath: strings.TrimSpace(stringFromAny(payload["log_path"])),
			}
		}
	}
	return nil
}

func latestWorkerDebugByWorker(events []orchestrator.EventEnvelope) map[string]tuiWorkerDebug {
	out := map[string]tuiWorkerDebug{}
	for _, event := range events {
		workerID := normalizeDebugWorkerID(event.WorkerID)
		if workerID == "" {
			continue
		}
		current, exists := out[workerID]
		if exists && event.TS.Before(current.TS) {
			continue
		}
		payload := decodeEventPayload(event.Payload)
		next := current
		next.WorkerID = workerID
		next.TaskID = strings.TrimSpace(event.TaskID)
		next.EventType = strings.TrimSpace(event.Type)
		next.TS = event.TS
		switch event.Type {
		case orchestrator.EventTypeTaskStarted:
			next.Message = strings.TrimSpace(stringFromAny(payload["goal"]))
		case orchestrator.EventTypeTaskProgress:
			next.Message = strings.TrimSpace(stringFromAny(payload["message"]))
			next.Step = intFromAny(payload["step"])
			if next.Step == 0 {
				next.Step = intFromAny(payload["steps"])
			}
			next.ToolCalls = intFromAny(payload["tool_calls"])
			if next.ToolCalls == 0 {
				next.ToolCalls = intFromAny(payload["tool_call"])
			}
			cmd := strings.TrimSpace(stringFromAny(payload["command"]))
			if cmd != "" {
				next.Command = cmd
			}
		case orchestrator.EventTypeTaskArtifact:
			next.Command = strings.TrimSpace(stringFromAny(payload["command"]))
			next.Args = append([]string{}, stringSliceFromAny(payload["args"])...)
			next.LogPath = strings.TrimSpace(stringFromAny(payload["path"]))
			next.Step = intFromAny(payload["step"])
			if next.ToolCalls == 0 {
				next.ToolCalls = intFromAny(payload["tool_call"])
			}
		case orchestrator.EventTypeTaskFailed:
			next.Reason = strings.TrimSpace(stringFromAny(payload["reason"]))
			next.Error = strings.TrimSpace(stringFromAny(payload["error"]))
			next.LogPath = strings.TrimSpace(stringFromAny(payload["log_path"]))
		case orchestrator.EventTypeWorkerStopped:
			next.Reason = strings.TrimSpace(stringFromAny(payload["status"]))
			next.Error = strings.TrimSpace(stringFromAny(payload["error"]))
			if logPath := strings.TrimSpace(stringFromAny(payload["log_path"])); logPath != "" {
				next.LogPath = logPath
			}
		}
		out[workerID] = next
	}
	return out
}

func normalizeDebugWorkerID(workerID string) string {
	trimmed := strings.TrimSpace(workerID)
	if trimmed == "" {
		return ""
	}
	return strings.TrimPrefix(trimmed, "signal-")
}

func latestTaskProgressByTask(events []orchestrator.EventEnvelope) map[string]tuiTaskProgress {
	progress := map[string]tuiTaskProgress{}
	for _, event := range events {
		taskID := strings.TrimSpace(event.TaskID)
		if taskID == "" {
			continue
		}
		switch event.Type {
		case orchestrator.EventTypeTaskStarted:
			payload := decodeEventPayload(event.Payload)
			msg := strings.TrimSpace(stringFromAny(payload["goal"]))
			if msg == "" {
				msg = "task started"
			}
			current, ok := progress[taskID]
			if !ok || event.TS.After(current.TS) {
				progress[taskID] = tuiTaskProgress{
					TS:      event.TS,
					Message: msg,
					Mode:    "start",
				}
			}
		case orchestrator.EventTypeTaskProgress:
			payload := decodeEventPayload(event.Payload)
			next := tuiTaskProgress{
				TS:        event.TS,
				Step:      intFromAny(payload["step"]),
				ToolCalls: intFromAny(payload["tool_calls"]),
				Message:   strings.TrimSpace(stringFromAny(payload["message"])),
				Mode:      strings.TrimSpace(stringFromAny(payload["mode"])),
			}
			if next.Step == 0 {
				next.Step = intFromAny(payload["steps"])
			}
			if next.ToolCalls == 0 {
				next.ToolCalls = intFromAny(payload["tool_call"])
			}
			if next.Message == "" {
				next.Message = strings.TrimSpace(stringFromAny(payload["type"]))
			}
			current, ok := progress[taskID]
			if !ok || event.TS.After(current.TS) || (event.TS.Equal(current.TS) && next.Step >= current.Step) {
				progress[taskID] = next
			}
		}
	}
	return progress
}

func activeTaskRows(rows []tuiTaskRow) []tuiTaskRow {
	active := make([]tuiTaskRow, 0, len(rows))
	for _, row := range rows {
		switch normalizeDisplayState(row.State) {
		case "running", "leased", "awaiting_approval":
			active = append(active, row)
		}
	}
	return active
}

func formatProgressSummary(progress tuiTaskProgress) string {
	parts := make([]string, 0, 3)
	if progress.Step > 0 {
		parts = append(parts, fmt.Sprintf("step %d", progress.Step))
	}
	if progress.Message != "" {
		parts = append(parts, progress.Message)
	}
	if progress.ToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("tools:%d", progress.ToolCalls))
	}
	return strings.Join(parts, " | ")
}

func decodeEventPayload(raw json.RawMessage) map[string]any {
	payload := map[string]any{}
	if len(raw) == 0 {
		return payload
	}
	_ = json.Unmarshal(raw, &payload)
	return payload
}

func stringFromAny(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return ""
	}
}

func intFromAny(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
		if parsed, err := value.Float64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return 0
}

func stringSliceFromAny(v any) []string {
	switch values := v.(type) {
	case []string:
		out := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(stringFromAny(value))
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return nil
	}
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

func parseTUICommand(raw string) (tuiCommand, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return tuiCommand{name: "refresh"}, nil
	}
	parts := strings.Fields(trimmed)
	cmd := strings.ToLower(parts[0])
	switch cmd {
	case "q", "quit", "exit":
		return tuiCommand{name: "quit"}, nil
	case "help", "h":
		return tuiCommand{name: "help"}, nil
	case "plan":
		return tuiCommand{name: "plan"}, nil
	case "tasks":
		return tuiCommand{name: "tasks"}, nil
	case "refresh", "r":
		return tuiCommand{name: "refresh"}, nil
	case "events":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: events <count>")
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil || n <= 0 {
			return tuiCommand{}, fmt.Errorf("events count must be a positive integer")
		}
		return tuiCommand{name: "events", eventLimit: n}, nil
	case "approve":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: approve <approval-id> [once|task|session] [reason]")
		}
		scope := "task"
		reasonStart := 2
		if len(parts) > 2 {
			switch strings.ToLower(parts[2]) {
			case "once", "task", "session":
				scope = strings.ToLower(parts[2])
				reasonStart = 3
			}
		}
		reason := "approved via tui"
		if len(parts) >= reasonStart+1 {
			reason = strings.Join(parts[reasonStart:], " ")
		}
		return tuiCommand{name: "approve", approval: parts[1], scope: scope, reason: reason}, nil
	case "deny":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: deny <approval-id> [reason]")
		}
		reason := "denied via tui"
		if len(parts) > 2 {
			reason = strings.Join(parts[2:], " ")
		}
		return tuiCommand{name: "deny", approval: parts[1], reason: reason}, nil
	case "stop":
		return tuiCommand{name: "stop"}, nil
	case "instruct":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: instruct <instruction>")
		}
		return tuiCommand{name: "instruct", reason: strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0]))}, nil
	case "ask":
		if len(parts) < 2 {
			return tuiCommand{}, fmt.Errorf("usage: ask <question>")
		}
		return tuiCommand{name: "ask", reason: strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0]))}, nil
	default:
		name := "instruct"
		if looksLikeQuestion(trimmed) {
			name = "ask"
		}
		return tuiCommand{name: name, reason: trimmed}, nil
	}
}

func looksLikeQuestion(input string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(input))
	if trimmed == "" {
		return false
	}
	if strings.HasSuffix(trimmed, "?") {
		return true
	}
	questionPrefixes := []string{
		"what", "why", "how", "when", "where", "who", "which",
		"can", "could", "would", "should", "is", "are", "do", "does", "did",
	}
	for _, prefix := range questionPrefixes {
		if strings.HasPrefix(trimmed, prefix+" ") {
			return true
		}
	}
	return false
}

func executeTUICommand(manager *orchestrator.Manager, runID string, eventLimit *int, cmd tuiCommand) (bool, string) {
	switch cmd.name {
	case "quit":
		return true, "exiting tui"
	case "help":
		return false, "commands: help, plan, tasks, ask <question>, instruct <text>, refresh, events <n>, approve <id> [scope] [reason], deny <id> [reason], stop, quit"
	case "plan":
		plan, err := manager.LoadRunPlan(runID)
		if err != nil {
			return false, "plan failed: " + err.Error()
		}
		goal := strings.TrimSpace(plan.Metadata.Goal)
		if goal == "" {
			goal = "(no goal metadata)"
		}
		return false, fmt.Sprintf("plan: goal=%q tasks=%d success=%d stop=%d", goal, len(plan.Tasks), len(plan.SuccessCriteria), len(plan.StopCriteria))
	case "tasks":
		plan, err := manager.LoadRunPlan(runID)
		if err != nil {
			return false, "tasks failed loading plan: " + err.Error()
		}
		leases, err := manager.ReadLeases(runID)
		if err != nil {
			return false, "tasks failed reading leases: " + err.Error()
		}
		rows := buildTaskRows(plan, leases)
		counts := map[string]int{}
		for _, row := range rows {
			counts[row.State]++
		}
		return false, fmt.Sprintf("tasks: total=%d running=%d awaiting=%d queued=%d failed=%d blocked=%d completed=%d", len(rows), counts["running"], counts["awaiting_approval"], counts["queued"], counts["failed"], counts["blocked"], counts["completed"])
	case "refresh":
		return false, "refreshed"
	case "events":
		if eventLimit != nil {
			*eventLimit = cmd.eventLimit
		}
		return false, fmt.Sprintf("event window set to %d", cmd.eventLimit)
	case "approve":
		if err := manager.SubmitApprovalDecision(runID, cmd.approval, true, cmd.scope, "tui", cmd.reason, 0); err != nil {
			return false, "approve failed: " + err.Error()
		}
		return false, fmt.Sprintf("approved %s (%s)", cmd.approval, cmd.scope)
	case "deny":
		if err := manager.SubmitApprovalDecision(runID, cmd.approval, false, "", "tui", cmd.reason, 0); err != nil {
			return false, "deny failed: " + err.Error()
		}
		return false, fmt.Sprintf("denied %s", cmd.approval)
	case "stop":
		if err := manager.Stop(runID); err != nil {
			return false, "stop failed: " + err.Error()
		}
		return false, "stop requested"
	case "ask":
		return false, answerTUIQuestion(manager, runID, cmd.reason)
	case "instruct":
		instruction := strings.TrimSpace(cmd.reason)
		if instruction == "" {
			return false, "instruct failed: instruction is required"
		}
		if err := manager.EmitEvent(runID, "operator", "", orchestrator.EventTypeOperatorInstruction, map[string]any{
			"instruction": instruction,
			"source":      "tui",
		}); err != nil {
			return false, "instruct failed: " + err.Error()
		}
		return false, "instruction queued: " + instruction
	default:
		return false, ""
	}
}

func answerTUIQuestion(manager *orchestrator.Manager, runID, question string) string {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return "ask failed: question is required"
	}
	lower := strings.ToLower(trimmed)

	status, statusErr := manager.Status(runID)
	if statusErr != nil {
		return "ask failed reading status: " + statusErr.Error()
	}
	plan, planErr := manager.LoadRunPlan(runID)
	leases, leaseErr := manager.ReadLeases(runID)
	if planErr != nil || leaseErr != nil {
		return fmt.Sprintf("status: state=%s workers=%d queued=%d running=%d", status.State, status.ActiveWorkers, status.QueuedTasks, status.RunningTasks)
	}

	rows := buildTaskRows(plan, leases)
	stateByTask := map[string]string{}
	for _, row := range rows {
		stateByTask[row.TaskID] = row.State
	}
	active := activeTaskRows(rows)

	if strings.Contains(lower, "plan") || strings.Contains(lower, "steps") {
		stepParts := make([]string, 0, len(plan.Tasks))
		for i, task := range plan.Tasks {
			state := stateByTask[task.TaskID]
			if state == "" {
				state = "queued"
			}
			stepParts = append(stepParts, fmt.Sprintf("%d)%s[%s]", i+1, task.Title, state))
			if len(stepParts) >= 10 && len(plan.Tasks) > 10 {
				stepParts = append(stepParts, fmt.Sprintf("... +%d more", len(plan.Tasks)-10))
				break
			}
		}
		return fmt.Sprintf("plan (%d steps): %s", len(plan.Tasks), strings.Join(stepParts, " | "))
	}

	if strings.Contains(lower, "current") || strings.Contains(lower, "working on") || strings.Contains(lower, "status") {
		if len(active) == 0 {
			return fmt.Sprintf("state=%s. no active step right now. queued=%d failed=%d completed=%d", status.State, countTaskState(rows, "queued"), countTaskState(rows, "failed"), countTaskState(rows, "completed"))
		}
		activeParts := make([]string, 0, len(active))
		for _, row := range active {
			activeParts = append(activeParts, fmt.Sprintf("%s[%s]", row.TaskID, row.State))
		}
		return fmt.Sprintf("state=%s. current active: %s", status.State, strings.Join(activeParts, " | "))
	}

	return "I can answer run-state questions (plan, steps, current status). Use `instruct <text>` to change the run."
}

func countTaskState(rows []tuiTaskRow, state string) int {
	target := normalizeDisplayState(state)
	count := 0
	for _, row := range rows {
		if normalizeDisplayState(row.State) == target {
			count++
		}
	}
	return count
}
