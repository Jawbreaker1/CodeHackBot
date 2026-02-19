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
	lines = append(lines, fitLine("", width))
	lines = append(lines, fitLine("Plan:", width))
	goal := strings.TrimSpace(snap.plan.Metadata.Goal)
	if goal == "" {
		goal = "(no goal metadata)"
	}
	lines = append(lines, fitLine("  - Goal: "+goal, width))
	lines = append(lines, fitLine(fmt.Sprintf("  - Tasks: %d | Success criteria: %d | Stop criteria: %d", len(snap.plan.Tasks), len(snap.plan.SuccessCriteria), len(snap.plan.StopCriteria)), width))
	lines = append(lines, fitLine("", width))
	lines = append(lines, fitLine("Execution:", width))
	stateByTaskID := map[string]tuiTaskRow{}
	counts := map[string]int{}
	for _, task := range snap.tasks {
		stateByTaskID[task.TaskID] = task
		counts[task.State]++
	}
	remainingSteps := counts["queued"] + counts["leased"] + counts["awaiting_approval"] + counts["blocked"]
	lines = append(lines, fitLine(fmt.Sprintf("  - Completed: %d/%d | Running: %d | Remaining: %d | Failed: %d", counts["completed"], len(snap.plan.Tasks), counts["running"], remainingSteps, counts["failed"]), width))
	activeRows := activeTaskRows(snap.tasks)
	if len(activeRows) == 0 {
		lines = append(lines, fitLine("  - Current: (no active task)", width))
	} else {
		for i, task := range activeRows {
			if i >= 2 {
				lines = append(lines, fitLine(fmt.Sprintf("  ... %d more active tasks", len(activeRows)-i), width))
				break
			}
			progress := formatProgressSummary(snap.progress[task.TaskID])
			if progress == "" {
				progress = "started; waiting for progress update"
			}
			lines = append(lines, fitLine(fmt.Sprintf("  - Current: %s | %s", task.TaskID, progress), width))
		}
	}
	lines = append(lines, fitLine("  - Next planned steps:", width))
	nextCount := 0
	for _, task := range snap.plan.Tasks {
		row, ok := stateByTaskID[task.TaskID]
		if !ok {
			continue
		}
		if row.State == "completed" || row.State == "failed" || row.State == "canceled" {
			continue
		}
		lines = append(lines, fitLine(fmt.Sprintf("    %d) %s [%s]", nextCount+1, task.Title, row.State), width))
		nextCount++
		if nextCount >= 4 {
			break
		}
	}
	if nextCount == 0 {
		lines = append(lines, fitLine("    (all planned steps complete or terminal)", width))
	}
	lines = append(lines, fitLine("", width))
	lines = append(lines, fitLine("Plan Steps:", width))
	if len(snap.plan.Tasks) == 0 {
		lines = append(lines, fitLine("  (no planned tasks)", width))
	} else {
		for i, task := range snap.plan.Tasks {
			row, ok := stateByTaskID[task.TaskID]
			state := "queued"
			if ok {
				state = row.State
			}
			lines = append(lines, fitLine(fmt.Sprintf("  %d. %s %s", i+1, stepMarker(state), task.Title), width))
			if state == "running" || state == "leased" || state == "awaiting_approval" {
				if progress := formatProgressSummary(snap.progress[task.TaskID]); progress != "" {
					lines = append(lines, fitLine("     now: "+progress, width))
				}
			}
			if i >= 7 && len(snap.plan.Tasks) > 8 {
				lines = append(lines, fitLine(fmt.Sprintf("  ... %d more planned steps", len(snap.plan.Tasks)-i-1), width))
				break
			}
		}
	}
	lines = append(lines, fitLine("", width))
	lines = append(lines, fitLine("Workers:", width))
	if len(snap.workers) == 0 {
		lines = append(lines, fitLine("  (no workers)", width))
	} else {
		for _, worker := range snap.workers {
			lines = append(lines, fitLine(fmt.Sprintf("  - %s | %s | seq=%d | task=%s", worker.WorkerID, worker.State, worker.LastSeq, worker.CurrentTask), width))
		}
	}
	lines = append(lines, fitLine("", width))
	lines = append(lines, fitLine("Task Board:", width))
	if len(snap.tasks) == 0 {
		lines = append(lines, fitLine("  (no tasks)", width))
	} else {
		for i, task := range snap.tasks {
			if i >= 12 {
				lines = append(lines, fitLine(fmt.Sprintf("  ... %d more", len(snap.tasks)-i), width))
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
			lines = append(lines, fitLine(fmt.Sprintf("  - %s [%s] worker=%s strategy=%s%s", task.TaskID, task.State, worker, strategy, dynamic), width))
		}
	}
	lines = append(lines, fitLine("", width))
	lines = append(lines, fitLine("Pending Approvals:", width))
	if len(snap.approvals) == 0 {
		lines = append(lines, fitLine("  (none)", width))
	} else {
		for _, approval := range snap.approvals {
			lines = append(lines, fitLine(fmt.Sprintf("  - %s | task=%s | tier=%s | %s", approval.ApprovalID, approval.TaskID, approval.RiskTier, approval.Reason), width))
		}
	}
	lines = append(lines, fitLine("", width))
	lines = append(lines, fitLine("Last Failure:", width))
	if snap.lastFailure == nil {
		lines = append(lines, fitLine("  (none)", width))
	} else {
		taskID := snap.lastFailure.TaskID
		if strings.TrimSpace(taskID) == "" {
			taskID = "-"
		}
		reason := snap.lastFailure.Reason
		if strings.TrimSpace(reason) == "" {
			reason = "task_failed"
		}
		lines = append(lines, fitLine(fmt.Sprintf("  - %s | task=%s | worker=%s | reason=%s", snap.lastFailure.TS.Format("15:04:05"), taskID, snap.lastFailure.Worker, reason), width))
		if hint := failureHint(reason); hint != "" {
			lines = append(lines, fitLine("    hint: "+hint, width))
		}
		if strings.TrimSpace(snap.lastFailure.Error) != "" {
			lines = append(lines, fitLine("    error: "+snap.lastFailure.Error, width))
		}
		if strings.TrimSpace(snap.lastFailure.LogPath) != "" {
			lines = append(lines, fitLine("    log: "+snap.lastFailure.LogPath, width))
		}
	}
	lines = append(lines, fitLine("", width))
	lines = append(lines, fitLine("Recent Events:", width))
	if len(snap.events) == 0 {
		lines = append(lines, fitLine("  (no events)", width))
	} else {
		for _, event := range snap.events {
			lines = append(lines, fitLine(fmt.Sprintf("  - %s | %s | worker=%s | task=%s", event.TS.Format("15:04:05"), event.Type, event.WorkerID, event.TaskID), width))
		}
	}
	lines = append(lines, fitLine("", width))
	lines = append(lines, fitLine("Command Log:", width))
	for _, msg := range messages {
		lines = append(lines, fitLine("  - "+msg, width))
	}

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
	const headKeep = 3
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
		return tuiCommand{name: "ask", reason: trimmed}, nil
	}
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
