package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
	"golang.org/x/term"
)

const (
	tuiStylePrompt = "\x1b[48;5;236m\x1b[38;5;252m"
	tuiStyleBar    = "\x1b[48;5;238m\x1b[38;5;250m"
	tuiStyleReset  = "\x1b[0m"

	tuiCommandLogStoreLines = 120
	tuiCommandLogViewLines  = 10
	tuiAskEventWindow       = 32
	tuiAskLLMMaxContext     = 7000
	tuiRecentEventsMax      = 6
)

type tuiCommand struct {
	name       string
	approval   string
	scope      string
	reason     string
	eventLimit int
	logCount   int
}

type tuiAssistantDecision struct {
	Reply            string `json:"reply"`
	QueueInstruction bool   `json:"queue_instruction"`
	Instruction      string `json:"instruction"`
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
	commandLogScroll := 0
	escSeq := []byte{}
	running := true
	for running {
		snapshot, snapErr := collectTUISnapshot(manager, runID, eventLimit)
		if snapErr != nil {
			messages = appendLogLines(messages, "snapshot error: "+snapErr.Error())
		}
		if exitOnDone && (snapshot.status.State == "completed" || snapshot.status.State == "stopped") {
			messages = appendLogLines(messages, fmt.Sprintf("run %s %s; exiting tui", runID, snapshot.status.State))
			renderTUI(stdout, runID, snapshot, messages, input, &commandLogScroll, eventLimit)
			break
		}
		renderTUI(stdout, runID, snapshot, messages, input, &commandLogScroll, eventLimit)

		select {
		case <-ticker.C:
			continue
		case readErr := <-errCh:
			if readErr != nil && readErr != io.EOF {
				messages = appendLogLines(messages, "input error: "+readErr.Error())
			}
			running = false
		case b := <-keyCh:
			if b == 0x1b || len(escSeq) > 0 {
				if b == 0x1b && len(escSeq) == 0 {
					escSeq = []byte{b}
					continue
				}
				escSeq = append(escSeq, b)
				if len(escSeq) == 2 && escSeq[1] != '[' {
					escSeq = nil
					continue
				}
				if len(escSeq) == 3 && escSeq[1] == '[' {
					if len(input) == 0 {
						switch escSeq[2] {
						case 'A':
							commandLogScroll++
						case 'B':
							if commandLogScroll > 0 {
								commandLogScroll--
							}
						}
					}
					escSeq = nil
					continue
				}
				if len(escSeq) > 3 {
					escSeq = nil
				}
				continue
			}
			switch b {
			case '\r', '\n':
				cmd, parseErr := parseTUICommand(input)
				input = ""
				if parseErr != nil {
					messages = appendLogLines(messages, "invalid command: "+parseErr.Error())
					commandLogScroll = 0
					continue
				}
				done, logLine := executeTUICommand(manager, runID, &eventLimit, cmd, &commandLogScroll)
				if logLine != "" {
					messages = appendLogLines(messages, logLine)
					commandLogScroll = 0
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
