package main

import (
	"flag"
	"fmt"
	"io"
	"os"
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
	status    orchestrator.RunStatus
	workers   []orchestrator.WorkerStatus
	approvals []orchestrator.PendingApprovalView
	events    []orchestrator.EventEnvelope
	updatedAt time.Time
}

func collectTUISnapshot(manager *orchestrator.Manager, runID string, eventLimit int) (tuiSnapshot, error) {
	var snap tuiSnapshot
	status, err := manager.Status(runID)
	if err != nil {
		return snap, err
	}
	workers, err := manager.Workers(runID)
	if err != nil {
		return snap, err
	}
	approvals, err := manager.PendingApprovals(runID)
	if err != nil {
		return snap, err
	}
	events, err := manager.Events(runID, eventLimit)
	if err != nil {
		return snap, err
	}
	snap.status = status
	snap.workers = workers
	snap.approvals = approvals
	snap.events = events
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
	lines = append(lines, fitLine("Workers:", width))
	if len(snap.workers) == 0 {
		lines = append(lines, fitLine("  (no workers)", width))
	} else {
		for _, worker := range snap.workers {
			lines = append(lines, fitLine(fmt.Sprintf("  - %s | %s | seq=%d | task=%s", worker.WorkerID, worker.State, worker.LastSeq, worker.CurrentTask), width))
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
	if len(lines) > maxBody {
		lines = lines[len(lines)-maxBody:]
	}

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
	default:
		return tuiCommand{}, fmt.Errorf("unknown command %q", parts[0])
	}
}

func executeTUICommand(manager *orchestrator.Manager, runID string, eventLimit *int, cmd tuiCommand) (bool, string) {
	switch cmd.name {
	case "quit":
		return true, "exiting tui"
	case "help":
		return false, "commands: help, refresh, events <n>, approve <id> [scope] [reason], deny <id> [reason], stop, quit"
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
	default:
		return false, ""
	}
}
