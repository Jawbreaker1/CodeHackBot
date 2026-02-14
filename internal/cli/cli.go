package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
	"github.com/Jawbreaker1/CodeHackBot/internal/msf"
	"github.com/Jawbreaker1/CodeHackBot/internal/plan"
	"github.com/Jawbreaker1/CodeHackBot/internal/playbook"
	"github.com/Jawbreaker1/CodeHackBot/internal/report"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"golang.org/x/term"
)

const (
	inputLineStyleStart  = "\x1b[48;5;236m\x1b[38;5;252m"
	inputLineStyleReset  = "\x1b[0m"
	llmIndicatorDelay    = 700 * time.Millisecond
	llmIndicatorInterval = 200 * time.Millisecond
)

type Runner struct {
	cfg                config.Config
	sessionID          string
	logger             *log.Logger
	reader             *bufio.Reader
	defaultConfigPath  string
	profilePath        string
	stopOnce           sync.Once
	currentTask        string
	currentTaskStart   time.Time
	currentMode        string
	planWizard         *planWizard
	history            []string
	historyIndex       int
	historyScratch     string
	llmMu              sync.Mutex
	llmInFlight        bool
	llmLabel           string
	llmStarted         time.Time
	pendingAssistGoal  string
	lastAssistCmdKey   string
	lastAssistCmdSeen  int
	lastAssistQuestion string
	lastAssistQSeen    int
	lastBrowseLogPath  string
	lastActionLogPath  string
	lastKnownTarget    string
	inputRenderLines   int
	llmGuard           llm.Guard
}

func NewRunner(cfg config.Config, sessionID, defaultConfigPath, profilePath string) *Runner {
	return &Runner{
		cfg:               cfg,
		sessionID:         sessionID,
		logger:            log.New(os.Stdout, fmt.Sprintf("[session:%s] ", sessionID), 0),
		reader:            bufio.NewReader(os.Stdin),
		defaultConfigPath: defaultConfigPath,
		profilePath:       profilePath,
		historyIndex:      -1,
		llmGuard:          llm.NewGuard(cfg.LLM.MaxFailures, time.Duration(cfg.LLM.CooldownSeconds)*time.Second),
	}
}

func (r *Runner) Run() error {
	r.printLogo()
	r.logger.Printf("BirdHackBot interactive mode. Type /help for commands.")
	for {
		line, err := r.readLine(r.prompt())
		if err != nil && err != io.EOF {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if err == io.EOF {
				r.Stop()
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, "/") {
			if err := r.handleCommand(line); err != nil {
				r.logger.Printf("Command error: %v", err)
			}
			if err == io.EOF {
				r.Stop()
				return nil
			}
			continue
		}
		if r.pendingAssistGoal != "" {
			if err := r.handleAssistFollowUp(line); err != nil {
				r.logger.Printf("Assist follow-up error: %v", err)
			}
			if err == io.EOF {
				r.Stop()
				return nil
			}
			continue
		}
		if r.planWizardActive() {
			if err := r.handlePlanWizardInput(line); err != nil {
				r.logger.Printf("Plan error: %v", err)
			}
			if err == io.EOF {
				r.Stop()
				return nil
			}
			continue
		}
		r.appendConversation("User", line)
		if assistErr := r.handleAssistGoal(line, false); assistErr != nil {
			r.logger.Printf("Assist error: %v", assistErr)
		}
		if err == io.EOF {
			r.Stop()
			return nil
		}
	}
}

func (r *Runner) handleCommand(line string) error {
	parts := strings.Fields(strings.TrimPrefix(line, "/"))
	if len(parts) == 0 {
		return nil
	}
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	if r.planWizardActive() && cmd != "plan" && cmd != "help" && cmd != "stop" && cmd != "exit" && cmd != "quit" {
		r.logger.Printf("Planning mode active. Use /plan done or /plan cancel.")
		return nil
	}

	switch cmd {
	case "help":
		r.printHelp()
	case "init":
		return r.handleInit(args)
	case "permissions":
		return r.handlePermissions(args)
	case "verbose":
		return r.handleVerbose(args)
	case "context":
		if len(args) > 0 && strings.ToLower(args[0]) == "show" {
			return r.handleContextShow()
		}
		r.logger.Printf("Context: max_recent=%d summarize_every=%d summarize_at=%d%%", r.cfg.Context.MaxRecentOutputs, r.cfg.Context.SummarizeEvery, r.cfg.Context.SummarizeAtPercent)
	case "ledger":
		return r.handleLedger(args)
	case "status":
		r.handleStatus()
	case "plan":
		return r.handlePlan(args)
	case "next":
		return r.handleNext(args)
	case "execute":
		return r.handleExecute(args)
	case "assist":
		return r.handleAssist(args)
	case "script":
		return r.handleScript(args)
	case "clean":
		return r.handleClean(args)
	case "ask":
		return r.handleAsk(strings.Join(args, " "))
	case "browse":
		return r.handleBrowse(args)
	case "summarize":
		return r.handleSummarize(args)
	case "run":
		return r.handleRun(args)
	case "report":
		return r.handleReport(args)
	case "msf":
		return r.handleMSF(args)
	case "resume":
		return r.handleResume()
	case "stop":
		r.Stop()
		return nil
	case "exit", "quit":
		r.Stop()
		os.Exit(0)
	default:
		r.logger.Printf("Unknown command: /%s", cmd)
	}
	return nil
}

func (r *Runner) handleInit(args []string) error {
	r.setTask("init")
	defer r.clearTask()
	createInventory := false
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "inventory":
			createInventory = true
		case "no-inventory":
			createInventory = false
		default:
			return fmt.Errorf("usage: /init [inventory|no-inventory]")
		}
	}

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: init not permitted")
	}

	if createInventory && r.cfg.Permissions.RequireApproval {
		approved, err := r.confirm("Run inventory commands now?")
		if err != nil {
			return err
		}
		if !approved {
			createInventory = false
		}
	}

	sessionDir, err := session.CreateScaffold(session.ScaffoldOptions{
		RootDir:           r.cfg.Session.LogDir,
		SessionID:         r.sessionID,
		PlanFilename:      r.cfg.Session.PlanFilename,
		InventoryFilename: r.cfg.Session.InventoryFilename,
		LedgerFilename:    r.cfg.Session.LedgerFilename,
		CreateLedger:      r.cfg.Session.LedgerEnabled,
	})
	if err != nil {
		return err
	}

	r.logger.Printf("Session scaffold created at %s", sessionDir)
	sessionConfigPath := config.SessionPath(r.cfg.Session.LogDir, r.sessionID)
	if err := config.Save(sessionConfigPath, r.cfg); err != nil {
		return fmt.Errorf("save session config: %w", err)
	}
	r.logger.Printf("Session config saved at %s", sessionConfigPath)

	if createInventory {
		if err := session.WriteInventory(sessionDir, r.cfg.Session.InventoryFilename, 5*time.Second); err != nil {
			return fmt.Errorf("inventory failed: %w", err)
		}
		r.logger.Printf("Inventory captured")
	}
	fmt.Print(renderInitSummary(r.currentTask, sessionDir, sessionConfigPath, createInventory))
	return nil
}

func (r *Runner) handlePermissions(args []string) error {
	if len(args) == 0 {
		r.logger.Printf("Permissions: %s (approval=%t)", r.cfg.Permissions.Level, r.cfg.Permissions.RequireApproval)
		return nil
	}
	level := strings.ToLower(args[0])
	switch level {
	case "readonly", "default", "all":
		r.cfg.Permissions.Level = level
		r.cfg.Permissions.RequireApproval = level == "default"
		r.logger.Printf("Permissions set to %s", level)
		return nil
	default:
		return fmt.Errorf("invalid permissions level: %s", level)
	}
}

func (r *Runner) handleVerbose(args []string) error {
	if len(args) == 0 {
		r.logger.Printf("Verbose logging: %t", r.cfg.UI.Verbose)
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "on", "true", "1":
		r.cfg.UI.Verbose = true
		r.logger.Printf("Verbose logging enabled")
	case "off", "false", "0":
		r.cfg.UI.Verbose = false
		r.logger.Printf("Verbose logging disabled")
	default:
		return fmt.Errorf("usage: /verbose on|off")
	}
	return nil
}

func (r *Runner) handleLedger(args []string) error {
	if len(args) == 0 {
		r.logger.Printf("Ledger enabled: %t", r.cfg.Session.LedgerEnabled)
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "on":
		r.cfg.Session.LedgerEnabled = true
		r.logger.Printf("Ledger enabled")
	case "off":
		r.cfg.Session.LedgerEnabled = false
		r.logger.Printf("Ledger disabled")
	default:
		return fmt.Errorf("usage: /ledger on|off")
	}
	return nil
}

func (r *Runner) handleStatus() {
	active, label, started := r.llmStatus()
	if r.currentTask == "" {
		if !active {
			r.logger.Printf("Status: idle")
			return
		}
		r.logger.Printf("Status: idle")
		r.logger.Printf("LLM: %s (%s)", label, formatElapsed(time.Since(started)))
		return
	}
	elapsed := formatElapsed(time.Since(r.currentTaskStart))
	r.logger.Printf("Status: running %s (%s)", r.currentTask, elapsed)
	if active {
		r.logger.Printf("LLM: %s (%s)", label, formatElapsed(time.Since(started)))
	}
}

func (r *Runner) handleSummarize(args []string) error {
	r.setTask("summarize")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: summarize not permitted")
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	manager := r.memoryManager(sessionDir)
	summarizer := r.summaryGenerator()

	reason := "manual"
	if len(args) > 0 {
		reason = strings.Join(args, " ")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("summarize")
	defer stopIndicator()
	if err := manager.Summarize(ctx, summarizer, reason); err != nil {
		return err
	}
	r.logger.Printf("Summary updated")
	return nil
}

func (r *Runner) handleContextShow() error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return err
	}
	summary := readFileTrimmed(artifacts.SummaryPath)
	facts := readFileTrimmed(artifacts.FactsPath)
	focus := readFileTrimmed(artifacts.FocusPath)
	state, _ := memory.LoadState(artifacts.StatePath)

	r.logger.Printf("Context Summary:\n%s", fallbackBlock(summary))
	r.logger.Printf("Known Facts:\n%s", fallbackBlock(facts))
	r.logger.Printf("Focus:\n%s", fallbackBlock(focus))
	r.logger.Printf("Context State: steps_since_summary=%d recent_logs=%d last_summary_at=%s", state.StepsSinceSummary, len(state.RecentLogs), state.LastSummaryAt)
	return nil
}

func (r *Runner) handlePlan(args []string) error {
	r.setTask("plan")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: plan updates not permitted")
	}

	if len(args) == 0 {
		return r.handlePlanWizardStart()
	}
	if len(args) > 0 {
		mode := strings.ToLower(args[0])
		if mode == "start" {
			return r.handlePlanWizardStart()
		}
		if mode == "done" || mode == "finish" {
			return r.handlePlanWizardDone()
		}
		if mode == "cancel" || mode == "exit" {
			return r.handlePlanWizardCancel()
		}
		if mode == "auto" || mode == "llm" {
			return r.handlePlanAuto(strings.Join(args[1:], " "))
		}
	}
	return r.handlePlanManual(args)
}

func (r *Runner) handlePlanManual(args []string) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}

	planText := strings.TrimSpace(strings.Join(args, " "))
	if planText == "" {
		r.logger.Printf("Enter plan text. End with a single '.' line.")
		lines := []string{}
		for {
			line, err := r.readLine("> ")
			if err != nil && err != io.EOF {
				return err
			}
			if strings.TrimSpace(line) == "." {
				break
			}
			lines = append(lines, line)
			if err == io.EOF {
				break
			}
		}
		planText = strings.TrimSpace(strings.Join(lines, "\n"))
	}

	if planText == "" {
		return fmt.Errorf("plan text is empty")
	}
	planPath, err := session.AppendPlan(sessionDir, r.cfg.Session.PlanFilename, planText)
	if err != nil {
		return err
	}
	r.logger.Printf("Plan updated: %s", planPath)
	return nil
}

func (r *Runner) handlePlanAuto(reason string) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	input, err := r.planInput(sessionDir)
	if err != nil {
		return err
	}
	if reason != "" {
		input.Goal = reason
	}
	input.Playbooks = r.playbookHints(reason)
	planner := r.planGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("plan")
	defer stopIndicator()
	content, err := planner.Plan(ctx, input)
	if err != nil {
		return err
	}
	if reason != "" {
		content = fmt.Sprintf("### Auto Plan (%s)\n\n%s", reason, content)
	}
	planPath, err := session.AppendPlan(sessionDir, r.cfg.Session.PlanFilename, content)
	if err != nil {
		return err
	}
	r.logger.Printf("Auto plan written: %s", planPath)
	return nil
}

func (r *Runner) handleNext(_ []string) error {
	r.setTask("next")
	defer r.clearTask()

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	input, err := r.planInput(sessionDir)
	if err != nil {
		return err
	}
	input.Playbooks = r.playbookHints(input.Plan)
	planner := r.planGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("next")
	defer stopIndicator()
	steps, err := planner.Next(ctx, input)
	if err != nil {
		return err
	}
	r.logger.Printf("Next steps:")
	for _, step := range steps {
		r.logger.Printf("- %s", step)
	}
	return nil
}

func (r *Runner) handleExecute(args []string) error {
	r.setTask("execute")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: execute not permitted")
	}

	mode := ""
	if len(args) > 0 {
		mode = strings.ToLower(args[0])
	}

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}

	if mode == "auto" || mode == "plan" {
		if err := r.handlePlanAuto("execute"); err != nil {
			return err
		}
	}

	input, err := r.planInput(sessionDir)
	if err != nil {
		return err
	}
	input.Playbooks = r.playbookHints(input.Plan)
	planner := r.planGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("next")
	defer stopIndicator()
	steps, err := planner.Next(ctx, input)
	if err != nil {
		return err
	}
	if len(steps) == 0 {
		return fmt.Errorf("no next steps available")
	}

	stepIndex := 0
	if len(args) > 1 {
		if value, err := strconv.Atoi(args[1]); err == nil && value > 0 && value <= len(steps) {
			stepIndex = value - 1
		}
	}
	step := steps[stepIndex]
	r.logger.Printf("Executing step: %s", step)
	return r.handleAssistGoal(step, false)
}

func (r *Runner) handleScript(args []string) error {
	r.setTask("script")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: scripts not permitted")
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: /script <py|sh> <name>")
	}
	lang := strings.ToLower(args[0])
	name := sanitizeFilename(args[1])
	if name == "" {
		return fmt.Errorf("invalid script name")
	}
	var ext string
	var interpreter string
	switch lang {
	case "py", "python":
		ext = ".py"
		interpreter = "python"
	case "sh", "bash":
		ext = ".sh"
		interpreter = "bash"
	default:
		return fmt.Errorf("unsupported script type: %s", lang)
	}

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	artifactsDir := filepath.Join(sessionDir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return fmt.Errorf("create artifacts dir: %w", err)
	}
	scriptPath := filepath.Join(artifactsDir, name+ext)

	r.logger.Printf("Enter %s script content. End with a single '.' line.", lang)
	lines := []string{}
	for {
		line, err := r.readLine("> ")
		if err != nil && err != io.EOF {
			return err
		}
		if strings.TrimSpace(line) == "." {
			break
		}
		lines = append(lines, line)
		if err == io.EOF {
			break
		}
	}
	content := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("script content is empty")
	}
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write script: %w", err)
	}
	r.logger.Printf("Script saved: %s", scriptPath)

	runArgs := []string{interpreter, scriptPath}
	return r.handleRun(runArgs)
}

func (r *Runner) handleClean(args []string) error {
	r.setTask("clean")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: clean not permitted")
	}

	days := 0
	if len(args) > 0 {
		value, err := strconv.Atoi(args[0])
		if err != nil || value < 0 {
			return fmt.Errorf("usage: /clean [days]")
		}
		days = value
	}

	root := r.cfg.Session.LogDir
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			r.logger.Printf("No sessions to clean.")
			return nil
		}
		return fmt.Errorf("read sessions: %w", err)
	}

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if days == 0 || info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(path); err != nil {
				r.logger.Printf("Failed to remove %s: %v", path, err)
				continue
			}
			removed++
		}
	}
	r.logger.Printf("Cleaned %d session(s).", removed)
	return nil
}

func (r *Runner) handleAssist(args []string) error {
	r.setTask("assist")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: assist not permitted")
	}
	dryRun := false
	goal := ""
	if len(args) > 0 && strings.ToLower(args[0]) == "dry" {
		dryRun = true
		goal = strings.Join(args[1:], " ")
	} else {
		goal = strings.Join(args, " ")
	}
	r.appendConversation("User", goal)
	return r.handleAssistGoal(goal, dryRun)
}

func (r *Runner) handleAsk(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if !r.llmAllowed() {
		r.logger.Printf("LLM unavailable; configure llm.base_url or wait for cooldown.")
		return nil
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	prompt := r.buildAskPrompt(sessionDir, text)
	r.appendConversation("User", text)
	client := llm.NewLMStudioClient(r.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("ask")
	defer stopIndicator()
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: 0.2,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: "You are BirdHackBot, a security testing assistant operating in an authorized lab owned by the user. Provide direct help within scope, including inspecting local files or assisting with encrypted files the user created. If access requires a password, ask for it; if asked to crack a password, proceed only when the user confirms authorization for that file. Do not emit tool-call markup; respond in plain text. If web access is needed, ask the user to run /browse with the URL (it requires approval). Answer clearly and concisely and ask follow-up questions when needed.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
	})
	if err != nil {
		r.recordLLMFailure(err)
		return err
	}
	r.recordLLMSuccess()
	if r.cfg.UI.Verbose {
		r.logger.Printf("Assistant response:")
	}
	fmt.Println(resp.Content)
	r.appendConversation("Assistant", resp.Content)
	return nil
}

func (r *Runner) handleRun(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /run <command> [args...]")
	}
	r.setTask(fmt.Sprintf("run %s", args[0]))
	defer r.clearTask()
	if !r.cfg.Tools.Shell.Enabled {
		return fmt.Errorf("shell execution disabled by config")
	}
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: run not permitted")
	}
	start := time.Now()
	requireApproval := r.cfg.Permissions.Level == "default" && r.cfg.Permissions.RequireApproval
	timeout := time.Duration(r.cfg.Tools.Shell.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if requireApproval {
		approved, err := r.confirm(fmt.Sprintf("Run command: %s %s?", args[0], strings.Join(args[1:], " ")))
		if err != nil {
			return err
		}
		if !approved {
			return fmt.Errorf("execution not approved")
		}
	}
	liveWriter := r.liveWriter()
	activityWriter := newActivityWriter(liveWriter)
	stopIndicator := r.startWorkingIndicator(activityWriter)
	defer stopIndicator()
	if activityWriter != nil {
		liveWriter = activityWriter
	}
	runner := exec.Runner{
		Permissions:      exec.PermissionLevel(r.cfg.Permissions.Level),
		RequireApproval:  false,
		LogDir:           filepath.Join(r.cfg.Session.LogDir, r.sessionID, "logs"),
		Timeout:          timeout,
		Reader:           r.reader,
		ScopeNetworks:    r.cfg.Scope.Networks,
		ScopeTargets:     r.cfg.Scope.Targets,
		ScopeDenyTargets: r.cfg.Scope.DenyTargets,
		LiveWriter:       liveWriter,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interruptCh, stopInterrupt, keyErr := startInterruptWatcher()
	if keyErr == nil {
		r.logger.Printf("Press ESC or Ctrl-C to interrupt")
	} else if r.isTTY() {
		r.logger.Printf("Ctrl-C to interrupt (ESC unavailable: %v)", keyErr)
	}
	if interruptCh != nil {
		go func() {
			<-interruptCh
			cancel()
		}()
	}

	result, err := runner.RunCommandWithContext(ctx, args[0], args[1:]...)
	wasCanceled := errors.Is(err, context.Canceled)
	if stopInterrupt != nil {
		stopInterrupt()
	}
	if result.LogPath != "" {
		r.logger.Printf("Log saved: %s", result.LogPath)
		r.recordActionArtifact(result.LogPath)
	}
	r.maybeAutoSummarize(result.LogPath, "run")
	ledgerStatus := "disabled"
	if r.cfg.Session.LedgerEnabled {
		if result.LogPath == "" {
			ledgerStatus = "skipped"
		} else {
			sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
			if ledgerErr := session.AppendLedger(sessionDir, r.cfg.Session.LedgerFilename, strings.Join(append([]string{args[0]}, args[1:]...), " "), result.LogPath, ""); ledgerErr != nil {
				r.logger.Printf("Ledger update failed: %v", ledgerErr)
				ledgerStatus = "error"
			} else {
				ledgerStatus = "appended"
			}
		}
	}
	fmt.Print(renderExecSummary(r.currentTask, args[0], args[1:], time.Since(start), result.LogPath, ledgerStatus, result.Output, err))
	if err != nil {
		if wasCanceled {
			err = fmt.Errorf("command interrupted")
			r.logger.Printf("Interrupted. What should I do differently?")
			return err
		}
		return commandError{Result: result, Err: err}
	}
	return nil
}

func (r *Runner) handleMSF(args []string) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: msf search not permitted")
	}
	if !r.cfg.Tools.Shell.Enabled {
		return fmt.Errorf("shell execution disabled by config")
	}
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: run not permitted")
	}
	if r.cfg.Tools.Metasploit.DiscoveryMode != "msfconsole" {
		if r.cfg.Tools.Metasploit.RPCEnabled {
			return fmt.Errorf("msfrpcd discovery not implemented; set discovery_mode to msfconsole")
		}
		return fmt.Errorf("metasploit discovery disabled by config")
	}

	query := msf.Query{}
	extra := []string{}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "service="):
			query.Service = strings.TrimPrefix(arg, "service=")
		case strings.HasPrefix(arg, "platform="):
			query.Platform = strings.TrimPrefix(arg, "platform=")
		case strings.HasPrefix(arg, "keyword="):
			query.Keyword = strings.TrimPrefix(arg, "keyword=")
		default:
			extra = append(extra, arg)
		}
	}
	if len(extra) > 0 {
		if query.Keyword == "" {
			query.Keyword = strings.Join(extra, " ")
		} else {
			query.Keyword = query.Keyword + " " + strings.Join(extra, " ")
		}
	}

	search := msf.BuildSearch(query)
	command := msf.BuildCommand(search)

	r.setTask("msf search")
	defer r.clearTask()

	if r.cfg.Permissions.RequireApproval {
		approved, err := r.confirm(fmt.Sprintf("Run msfconsole search: %s?", search))
		if err != nil {
			return err
		}
		if !approved {
			return fmt.Errorf("execution not approved")
		}
	}

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interruptCh, stopInterrupt, keyErr := startInterruptWatcher()
	if keyErr == nil {
		r.logger.Printf("Press ESC or Ctrl-C to interrupt")
	} else if r.isTTY() {
		r.logger.Printf("Ctrl-C to interrupt (ESC unavailable: %v)", keyErr)
	}
	if interruptCh != nil {
		go func() {
			<-interruptCh
			cancel()
		}()
	}

	liveWriter := r.liveWriter()
	activityWriter := newActivityWriter(liveWriter)
	stopIndicator := r.startWorkingIndicator(activityWriter)
	defer stopIndicator()
	if activityWriter != nil {
		liveWriter = activityWriter
	}
	execRunner := exec.Runner{
		Permissions:      exec.PermissionLevel(r.cfg.Permissions.Level),
		RequireApproval:  false,
		LogDir:           filepath.Join(r.cfg.Session.LogDir, r.sessionID, "logs"),
		Timeout:          2 * time.Minute,
		Reader:           r.reader,
		ScopeNetworks:    r.cfg.Scope.Networks,
		ScopeTargets:     r.cfg.Scope.Targets,
		ScopeDenyTargets: r.cfg.Scope.DenyTargets,
		LiveWriter:       liveWriter,
	}
	cmdArgs := []string{"-q", "-x", command}
	result, err := execRunner.RunCommandWithContext(ctx, "msfconsole", cmdArgs...)
	wasCanceled := errors.Is(err, context.Canceled)
	if stopInterrupt != nil {
		stopInterrupt()
	}
	if result.LogPath != "" {
		r.logger.Printf("Log saved: %s", result.LogPath)
		r.recordActionArtifact(result.LogPath)
	}
	r.maybeAutoSummarize(result.LogPath, "msf")

	fmt.Print(renderExecSummary(r.currentTask, "msfconsole", cmdArgs, time.Since(start), result.LogPath, "disabled", result.Output, err))
	if err != nil {
		if wasCanceled {
			r.logger.Printf("Interrupted. What should I do differently?")
			return fmt.Errorf("command interrupted")
		}
		return err
	}

	lines := msf.ParseSearchOutput(result.Output)
	if len(lines) == 0 {
		r.logger.Printf("No modules found")
		return nil
	}
	r.logger.Printf("Modules:")
	for _, line := range lines {
		r.logger.Printf("%s", line)
	}
	return nil
}

func (r *Runner) handleReport(args []string) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: report generation not permitted")
	}
	outPath := filepath.Join(r.cfg.Session.LogDir, r.sessionID, "report.md")
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		outPath = args[0]
	}
	info := report.Info{
		Scope:     r.cfg.Scope.Targets,
		SessionID: r.sessionID,
	}
	if r.cfg.Session.LedgerEnabled {
		ledgerPath := filepath.Join(r.cfg.Session.LogDir, r.sessionID, r.cfg.Session.LedgerFilename)
		info.Ledger = readFileTrimmed(ledgerPath)
	}
	if err := report.Generate("", outPath, info); err != nil {
		return err
	}
	r.logger.Printf("Report generated: %s", outPath)
	return nil
}

func (r *Runner) handleResume() error {
	root := r.cfg.Session.LogDir
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			r.logger.Printf("No sessions found. Run /init to create one.")
			return nil
		}
		return fmt.Errorf("read sessions: %w", err)
	}
	r.logger.Printf("Available sessions:")
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		r.logger.Printf("- %s", entry.Name())
	}
	line, err := r.readLine("Enter session id to resume (or blank to cancel): ")
	if err != nil && err != io.EOF {
		return fmt.Errorf("read session id: %w", err)
	}
	selection := strings.TrimSpace(line)
	if selection == "" {
		return nil
	}
	r.sessionID = selection
	r.logger.SetPrefix(fmt.Sprintf("[session:%s] ", r.sessionID))
	sessionConfigPath := config.SessionPath(r.cfg.Session.LogDir, r.sessionID)
	if _, err := os.Stat(sessionConfigPath); err == nil {
		if cfg, _, err := config.Load(r.defaultConfigPath, r.profilePath, sessionConfigPath); err == nil {
			r.cfg = cfg
			r.logger.Printf("Session %s loaded from %s", r.sessionID, sessionConfigPath)
		} else {
			r.logger.Printf("Session switched to %s. Config load failed: %v", r.sessionID, err)
		}
	} else if os.IsNotExist(err) {
		r.logger.Printf("Session switched to %s. No config snapshot found.", r.sessionID)
	} else {
		return fmt.Errorf("stat session config: %w", err)
	}
	return nil
}

func (r *Runner) handleStop() error {
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	if _, err := os.Stat(sessionDir); err != nil {
		if os.IsNotExist(err) {
			r.logger.Printf("No session directory found for %s", r.sessionID)
			return nil
		}
		return fmt.Errorf("stat session dir: %w", err)
	}
	if err := session.CloseMeta(sessionDir, r.sessionID); err != nil {
		return err
	}
	r.logger.Printf("Session %s closed", r.sessionID)
	r.logger.Printf("Resume with: %s --resume %s", filepath.Base(os.Args[0]), r.sessionID)
	return nil
}

func (r *Runner) ensureSessionScaffold() (string, error) {
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	if _, err := os.Stat(sessionDir); err == nil {
		return sessionDir, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat session dir: %w", err)
	}
	if _, err := session.CreateScaffold(session.ScaffoldOptions{
		RootDir:           r.cfg.Session.LogDir,
		SessionID:         r.sessionID,
		PlanFilename:      r.cfg.Session.PlanFilename,
		InventoryFilename: r.cfg.Session.InventoryFilename,
		LedgerFilename:    r.cfg.Session.LedgerFilename,
		CreateLedger:      r.cfg.Session.LedgerEnabled,
	}); err != nil {
		return "", err
	}
	sessionConfigPath := config.SessionPath(r.cfg.Session.LogDir, r.sessionID)
	if _, err := os.Stat(sessionConfigPath); os.IsNotExist(err) {
		if err := config.Save(sessionConfigPath, r.cfg); err != nil {
			return "", fmt.Errorf("save session config: %w", err)
		}
	}
	return sessionDir, nil
}

func (r *Runner) Stop() {
	r.stopOnce.Do(func() {
		if err := r.handleStop(); err != nil {
			r.logger.Printf("Session stop error: %v", err)
		}
	})
}

func (r *Runner) printHelp() {
	r.logger.Printf("Commands: /init /permissions /verbose /context [/show] /ledger /status /plan /next /execute /assist /script /clean /ask /browse /summarize /run /msf /report /resume /stop /exit")
	r.logger.Printf("Example: /permissions readonly")
	r.logger.Printf("Plain text routes to /assist. Use /ask for explicit non-agentic chat.")
	r.logger.Printf("/plan starts guided planning; /plan done or /plan cancel ends it.")
	r.logger.Printf("Session logs live under: %s", filepath.Clean(r.cfg.Session.LogDir))
}

func (r *Runner) printLogo() {
	data, err := os.ReadFile(filepath.Join("assets", "logo.ascii"))
	if err != nil {
		return
	}
	fmt.Println(string(data))
}

func (r *Runner) memoryManager(sessionDir string) memory.Manager {
	return memory.Manager{
		SessionDir:         sessionDir,
		LogDir:             filepath.Join(sessionDir, "logs"),
		PlanFilename:       r.cfg.Session.PlanFilename,
		LedgerFilename:     r.cfg.Session.LedgerFilename,
		LedgerEnabled:      r.cfg.Session.LedgerEnabled,
		MaxRecentOutputs:   r.cfg.Context.MaxRecentOutputs,
		SummarizeEvery:     r.cfg.Context.SummarizeEvery,
		SummarizeAtPercent: r.cfg.Context.SummarizeAtPercent,
		ChatHistoryLines:   r.cfg.Context.ChatHistoryLines,
	}
}

func (r *Runner) summaryGenerator() memory.Summarizer {
	primary := memory.LLMSummarizer{Client: llm.NewLMStudioClient(r.cfg), Model: r.cfg.LLM.Model}
	fallback := memory.FallbackSummarizer{}
	return guardedSummarizer{
		allow:     r.llmAllowed,
		onSuccess: r.recordLLMSuccess,
		onFailure: r.recordLLMFailure,
		primary:   primary,
		fallback:  fallback,
	}
}

func (r *Runner) maybeAutoSummarize(logPath, reason string) {
	if logPath == "" {
		return
	}
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	manager := r.memoryManager(sessionDir)
	state, err := manager.RecordLog(logPath)
	if err != nil {
		r.logger.Printf("Context tracking failed: %v", err)
		return
	}
	if !manager.ShouldSummarize(state) {
		return
	}
	if r.cfg.Permissions.Level == "readonly" {
		r.logger.Printf("Auto-summarize skipped (readonly)")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("summarize")
	defer stopIndicator()
	if err := manager.Summarize(ctx, r.summaryGenerator(), reason); err != nil {
		r.logger.Printf("Auto-summarize failed: %v", err)
		return
	}
	r.logger.Printf("Auto-summary updated")
}

func (r *Runner) planInput(sessionDir string) (plan.Input, error) {
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return plan.Input{}, err
	}
	summaryText := readFileTrimmed(artifacts.SummaryPath)
	facts, _ := memory.ReadBullets(artifacts.FactsPath)
	inventoryPath := filepath.Join(sessionDir, r.cfg.Session.InventoryFilename)
	inventory := readFileTrimmed(inventoryPath)
	planPath := filepath.Join(sessionDir, r.cfg.Session.PlanFilename)
	planText := readFileTrimmed(planPath)

	return plan.Input{
		SessionID:  r.sessionID,
		Scope:      r.cfg.Scope.Networks,
		Targets:    r.cfg.Scope.Targets,
		Summary:    summaryText,
		KnownFacts: facts,
		Inventory:  inventory,
		Plan:       planText,
	}, nil
}

func (r *Runner) planGenerator() plan.Planner {
	llmPlanner := plan.LLMPlanner{Client: llm.NewLMStudioClient(r.cfg), Model: r.cfg.LLM.Model}
	fallback := plan.FallbackPlanner{}
	return guardedPlanner{
		allow:     r.llmAllowed,
		onSuccess: r.recordLLMSuccess,
		onFailure: r.recordLLMFailure,
		primary:   llmPlanner,
		fallback:  fallback,
	}
}

func (r *Runner) llmAvailable() bool {
	baseURL := strings.TrimSpace(r.cfg.LLM.BaseURL)
	if baseURL != "" {
		return true
	}
	return !r.cfg.Network.AssumeOffline
}

func (r *Runner) llmAllowed() bool {
	if !r.llmAvailable() {
		return false
	}
	return r.llmGuard.Allow()
}

func (r *Runner) recordLLMFailure(err error) {
	if err == nil {
		return
	}
	r.llmGuard.RecordFailure()
	if !r.llmGuard.Allow() {
		until := r.llmGuard.DisabledUntil()
		if !until.IsZero() {
			r.logger.Printf("LLM disabled until %s after %d failures.", until.Format(time.RFC3339), r.llmGuard.Failures())
		}
	}
}

func (r *Runner) recordLLMSuccess() {
	r.llmGuard.RecordSuccess()
}

func readFileTrimmed(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (r *Runner) assistInput(sessionDir, goal, mode string) (assist.Input, error) {
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return assist.Input{}, err
	}
	summaryText := readFileTrimmed(artifacts.SummaryPath)
	facts, _ := memory.ReadBullets(artifacts.FactsPath)
	focusText := readFileTrimmed(artifacts.FocusPath)
	history := r.readChatHistory(artifacts.ChatPath)
	workingDir := r.currentWorkingDir()
	recentLog := r.readRecentLogSnippet(artifacts)
	playbooks := r.playbookHints(goal)
	planPath := filepath.Join(sessionDir, r.cfg.Session.PlanFilename)
	inventoryPath := filepath.Join(sessionDir, r.cfg.Session.InventoryFilename)
	targets := append([]string{}, r.cfg.Scope.Targets...)
	if len(targets) == 0 {
		if target := strings.TrimSpace(r.bestKnownTarget()); target != "" {
			targets = append(targets, target)
		}
	}
	return assist.Input{
		SessionID:   r.sessionID,
		Scope:       r.cfg.Scope.Networks,
		Targets:     targets,
		Summary:     summaryText,
		KnownFacts:  facts,
		Focus:       focusText,
		ChatHistory: history,
		WorkingDir:  workingDir,
		RecentLog:   recentLog,
		Playbooks:   playbooks,
		Mode:        mode,
		Plan:        readFileTrimmed(planPath),
		Inventory:   readFileTrimmed(inventoryPath),
		Goal:        strings.TrimSpace(goal),
	}, nil
}

func (r *Runner) buildAskPrompt(sessionDir, question string) string {
	artifacts, _ := memory.EnsureArtifacts(sessionDir)
	summary := readFileTrimmed(artifacts.SummaryPath)
	facts := readFileTrimmed(artifacts.FactsPath)
	focus := readFileTrimmed(artifacts.FocusPath)
	history := r.readChatHistory(artifacts.ChatPath)
	recentLogs := r.readRecentLogSnippets(artifacts, 3)
	planPath := filepath.Join(sessionDir, r.cfg.Session.PlanFilename)
	inventoryPath := filepath.Join(sessionDir, r.cfg.Session.InventoryFilename)

	builder := strings.Builder{}
	builder.WriteString("User question: " + question + "\n")
	if history != "" {
		builder.WriteString("\nRecent conversation:\n" + history + "\n")
	}
	if recentLogs != "" {
		builder.WriteString("\nRecent log snippets:\n" + recentLogs + "\n")
	}
	if summary != "" {
		builder.WriteString("\nSummary:\n" + summary + "\n")
	}
	if facts != "" {
		builder.WriteString("\nKnown facts:\n" + facts + "\n")
	}
	if focus != "" {
		builder.WriteString("\nFocus:\n" + focus + "\n")
	}
	plan := readFileTrimmed(planPath)
	if plan != "" {
		builder.WriteString("\nPlan:\n" + plan + "\n")
	}
	inventory := readFileTrimmed(inventoryPath)
	if inventory != "" {
		builder.WriteString("\nInventory:\n" + inventory + "\n")
	}
	return builder.String()
}

func (r *Runner) readChatHistory(path string) string {
	if r.cfg.Context.ChatHistoryLines <= 0 || path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	lines = filterNonEmpty(lines)
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > r.cfg.Context.ChatHistoryLines {
		lines = lines[len(lines)-r.cfg.Context.ChatHistoryLines:]
	}
	return strings.Join(lines, "\n")
}

func (r *Runner) appendChatHistory(path, role, content string) {
	if r.cfg.Context.ChatHistoryLines <= 0 || path == "" {
		return
	}
	clean := strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
	if clean == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s: %s\n", role, clean)
}

func filterNonEmpty(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func (r *Runner) currentWorkingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func (r *Runner) readRecentLogSnippet(artifacts memory.Artifacts) string {
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		return ""
	}
	if len(state.RecentLogs) == 0 {
		return ""
	}
	path := state.RecentLogs[len(state.RecentLogs)-1]
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	const maxBytes = 2000
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	return fmt.Sprintf("[log: %s]\n%s", path, content)
}

func (r *Runner) readRecentLogSnippets(artifacts memory.Artifacts, maxLogs int) string {
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		return ""
	}
	if len(state.RecentLogs) == 0 {
		return ""
	}
	paths := state.RecentLogs
	if maxLogs > 0 && len(paths) > maxLogs {
		paths = paths[len(paths)-maxLogs:]
	}
	const maxBytes = 1200
	builder := strings.Builder{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > maxBytes {
			data = data[len(data)-maxBytes:]
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("[log: %s]\n%s\n", path, content))
	}
	return strings.TrimSpace(builder.String())
}

func (r *Runner) playbookHints(goal string) string {
	if r.cfg.Context.PlaybookMax == 0 {
		return ""
	}
	entries, err := playbook.Load(filepath.Join("docs", "playbooks"))
	if err != nil || len(entries) == 0 {
		return ""
	}
	text := strings.TrimSpace(goal)
	if text == "" {
		return ""
	}
	matches := playbook.Match(entries, text, r.cfg.Context.PlaybookMax)
	if len(matches) == 0 {
		lower := strings.ToLower(text)
		if strings.Contains(lower, "playbook") || strings.Contains(lower, "workflow") || strings.Contains(lower, "procedure") {
			return playbook.List(entries)
		}
		return ""
	}
	return playbook.Render(matches, r.cfg.Context.PlaybookLines)
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	builder := strings.Builder{}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func fallbackBlock(content string) string {
	if strings.TrimSpace(content) == "" {
		return "(empty)"
	}
	return content
}

func (r *Runner) assistGenerator() assist.Assistant {
	llmAssistant := assist.LLMAssistant{Client: llm.NewLMStudioClient(r.cfg), Model: r.cfg.LLM.Model}
	fallback := assist.FallbackAssistant{}
	return guardedAssistant{
		allow:     r.llmAllowed,
		onSuccess: r.recordLLMSuccess,
		onFailure: r.recordLLMFailure,
		primary:   llmAssistant,
		fallback:  fallback,
	}
}

func (r *Runner) getAssistSuggestion(goal string, mode string) (assist.Suggestion, error) {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return assist.Suggestion{}, err
	}
	input, err := r.assistInput(sessionDir, r.enrichAssistGoal(goal, mode), mode)
	if err != nil {
		return assist.Suggestion{}, err
	}
	assistant := r.assistGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return assistant.Suggest(ctx, input)
}

func (r *Runner) handleAssistGoal(goal string, dryRun bool) error {
	trimmedGoal := strings.TrimSpace(goal)
	if trimmedGoal != "" {
		r.updateKnownTargetFromText(trimmedGoal)
		r.updateTaskFoundation(trimmedGoal)
		r.pendingAssistGoal = ""
		r.resetAssistLoopState()
	}
	if !dryRun && isSummaryIntent(trimmedGoal) && strings.TrimSpace(r.lastActionLogPath) != "" {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Using latest artifact for summary: %s", r.lastActionLogPath)
		}
		return r.summarizeFromLatestArtifact(trimmedGoal)
	}
	return r.handleAssistGoalWithMode(goal, dryRun, "")
}

func (r *Runner) handleAssistGoalWithMode(goal string, dryRun bool, mode string) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: assist not permitted")
	}
	if mode == "execute-step" {
		return r.handleAssistSingleStep(goal, dryRun, mode)
	}
	return r.handleAssistAgentic(goal, dryRun, mode)
}

func (r *Runner) handleAssistSingleStep(goal string, dryRun bool, mode string) error {
	stopIndicator := r.startLLMIndicatorIfAllowed("assist")
	defer stopIndicator()
	suggestion, err := r.getAssistSuggestion(goal, mode)
	if err != nil {
		return err
	}
	if suggestion.Type == "noop" && strings.TrimSpace(goal) != "" {
		return r.handleAssistNoop(goal, dryRun)
	}
	if err := r.executeAssistSuggestion(suggestion, dryRun); err != nil {
		if r.handleAssistCommandFailure(goal, suggestion, err) {
			r.maybeEmitGoalSummary(goal, dryRun)
			return nil
		}
		return err
	}
	if !dryRun && suggestion.Type == "command" {
		r.pendingAssistGoal = ""
		r.maybeSuggestNextSteps(goal, suggestion)
	}
	return nil
}

func (r *Runner) handleAssistAgentic(goal string, dryRun bool, mode string) error {
	if mode != "web-agentic" {
		if url := extractFirstURL(goal); url != "" && shouldAutoBrowse(goal) {
			return r.handleAssistAgentic(fmt.Sprintf("%s (fetch and analyze this URL, then summarize)", goal), dryRun, "web-agentic")
		}
	}

	maxSteps := r.assistMaxSteps()
	stepsRun := 0
	stepMode := mode
	lastCommand := assist.Suggestion{}
	for {
		if maxSteps > 0 && stepsRun >= maxSteps {
			r.logger.Printf("Reached max steps (%d).", maxSteps)
			if !dryRun && lastCommand.Type == "command" {
				r.maybeSuggestNextSteps(goal, lastCommand)
			}
			r.maybeEmitGoalSummary(goal, dryRun)
			return nil
		}
		if maxSteps > 0 {
			r.logger.Printf("Assistant step %d/%d", stepsRun+1, maxSteps)
		} else {
			r.logger.Printf("Assistant step %d", stepsRun+1)
		}
		label := "assist"
		if maxSteps > 0 {
			label = fmt.Sprintf("assist %d/%d", stepsRun+1, maxSteps)
		}
		stopIndicator := r.startLLMIndicatorIfAllowed(label)
		suggestion, err := r.getAssistSuggestion(goal, stepMode)
		stopIndicator()
		if err != nil {
			return err
		}
		if suggestion.Type == "noop" && strings.TrimSpace(goal) != "" {
			return r.handleAssistNoop(goal, dryRun)
		}
		if suggestion.Type == "plan" {
			if err := r.handlePlanSuggestion(suggestion, dryRun); err != nil {
				return err
			}
			r.maybeEmitGoalSummary(goal, dryRun)
			return nil
		}
		if err := r.executeAssistSuggestion(suggestion, dryRun); err != nil {
			if r.handleAssistCommandFailure(goal, suggestion, err) {
				r.maybeEmitGoalSummary(goal, dryRun)
				return nil
			}
			return err
		}
		if dryRun {
			return nil
		}
		if suggestion.Type == "question" {
			r.pendingAssistGoal = goal
			return nil
		}
		if suggestion.Type == "command" {
			lastCommand = suggestion
			r.pendingAssistGoal = ""
		}
		stepsRun++
		stepMode = "execute-step"
	}
}

func (r *Runner) handleAssistNoop(goal string, dryRun bool) error {
	clarifyGoal := fmt.Sprintf("Original goal: %s\nThe previous suggestion was noop. Provide one concrete next step. If details are missing, ask one concise clarifying question.", goal)
	stopIndicator := r.startLLMIndicatorIfAllowed("assist clarify")
	defer stopIndicator()
	suggestion, err := r.getAssistSuggestion(clarifyGoal, "recover")
	if err != nil {
		r.pendingAssistGoal = goal
		fmt.Println("I need one more detail to continue. Share what target/path/url to act on.")
		return nil
	}
	if suggestion.Type == "noop" {
		r.pendingAssistGoal = goal
		fmt.Println("I need one more detail to continue. Share what target/path/url to act on.")
		return nil
	}
	if err := r.executeAssistSuggestion(suggestion, dryRun); err != nil {
		if r.handleAssistCommandFailure(goal, suggestion, err) {
			return nil
		}
		return err
	}
	if suggestion.Type == "question" {
		r.pendingAssistGoal = goal
	} else if suggestion.Type == "command" {
		r.pendingAssistGoal = ""
	}
	return nil
}

func (r *Runner) handleAssistFollowUp(answer string) error {
	goal := strings.TrimSpace(r.pendingAssistGoal)
	r.pendingAssistGoal = ""
	if goal == "" {
		return nil
	}
	if shouldStartBaselineScan(answer) && strings.TrimSpace(r.lastKnownTarget) != "" {
		target := r.bestKnownTarget()
		if target != "" {
			r.logger.Printf("Using remembered target for baseline non-intrusive scan: %s", target)
			return r.handleRun([]string{"nmap", "-sV", "-Pn", "--top-ports", "100", target})
		}
	}
	combined := fmt.Sprintf("Original goal: %s\nUser answer to previous assistant question: %s\nContinue the task using available tools.", goal, strings.TrimSpace(answer))
	if target := strings.TrimSpace(r.lastKnownTarget); target != "" {
		combined += fmt.Sprintf("\nCurrent remembered target: %s", target)
	}
	return r.handleAssistGoalWithMode(combined, false, "follow-up")
}

func (r *Runner) assistMaxSteps() int {
	if r.cfg.Agent.MaxSteps > 0 {
		return r.cfg.Agent.MaxSteps
	}
	return 6
}

func (r *Runner) executeAssistSuggestion(suggestion assist.Suggestion, dryRun bool) error {
	if isPlaceholderCommand(suggestion.Command) {
		return fmt.Errorf("assistant returned placeholder command: %s", suggestion.Command)
	}
	switch suggestion.Type {
	case "question":
		if suggestion.Question == "" {
			return fmt.Errorf("assistant returned empty question")
		}
		if err := r.guardAssistQuestionLoop(suggestion.Question); err != nil {
			return err
		}
		if r.cfg.UI.Verbose {
			r.logger.Printf("Assistant question: %s", suggestion.Question)
			if suggestion.Summary != "" {
				r.logger.Printf("Summary: %s", suggestion.Summary)
			}
		} else {
			fmt.Println(suggestion.Question)
		}
		r.appendConversation("Assistant", suggestion.Question)
		return nil
	case "noop":
		if r.cfg.UI.Verbose {
			r.logger.Printf("Assistant has no suggestion")
		}
		return nil
	case "plan":
		r.resetAssistLoopState()
		return r.handlePlanSuggestion(suggestion, dryRun)
	case "command":
		if suggestion.Command == "" {
			return fmt.Errorf("assistant returned empty command")
		}
	default:
		return fmt.Errorf("assistant returned unknown type: %s", suggestion.Type)
	}

	r.logger.Printf("Suggested command: %s %s", suggestion.Command, strings.Join(suggestion.Args, " "))
	r.appendConversation("Assistant", fmt.Sprintf("Suggested command: %s %s", suggestion.Command, strings.Join(suggestion.Args, " ")))
	if r.cfg.UI.Verbose {
		if suggestion.Summary != "" {
			r.logger.Printf("Summary: %s", suggestion.Summary)
		}
		if suggestion.Risk != "" {
			r.logger.Printf("Risk: %s", suggestion.Risk)
		}
	}
	if dryRun {
		return nil
	}
	if err := r.guardAssistCommandLoop(suggestion.Command, suggestion.Args); err != nil {
		return err
	}
	if strings.EqualFold(suggestion.Command, "browse") {
		args, err := sanitizeBrowseArgs(suggestion.Args)
		if err != nil {
			return err
		}
		return r.handleBrowse(args)
	}
	if strings.HasPrefix(strings.ToLower(suggestion.Command), "http") && len(suggestion.Args) == 0 {
		return r.handleBrowse([]string{suggestion.Command})
	}
	args := append([]string{suggestion.Command}, suggestion.Args...)
	err := r.handleRun(args)
	if isBenignNoMatchError(err) {
		r.logger.Printf("No matches found for this step; continuing.")
		return nil
	}
	return err
}

func (r *Runner) handlePlanSuggestion(suggestion assist.Suggestion, dryRun bool) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	planText := strings.TrimSpace(suggestion.Plan)
	if planText == "" && len(suggestion.Steps) > 0 {
		builder := strings.Builder{}
		builder.WriteString("## Plan (Assistant)\n\n### Steps\n")
		for i, step := range suggestion.Steps {
			builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
		}
		planText = builder.String()
	}
	if planText != "" {
		planPath, err := session.AppendPlan(sessionDir, r.cfg.Session.PlanFilename, planText)
		if err != nil {
			return err
		}
		if r.cfg.UI.Verbose {
			r.logger.Printf("Plan updated: %s", planPath)
		}
	}
	if planText != "" {
		fmt.Println("Plan:")
		fmt.Println(planText)
		r.appendConversation("Assistant", planText)
	}
	if len(suggestion.Steps) > 0 {
		fmt.Println("Plan steps:")
		steps := suggestion.Steps
		maxSteps := r.assistMaxSteps()
		if maxSteps > 0 && len(steps) > maxSteps {
			steps = steps[:maxSteps]
		}
		for i, step := range steps {
			fmt.Printf("%d) %s\n", i+1, step)
		}
		r.appendConversation("Assistant", "Plan steps: "+strings.Join(steps, " | "))
		suggestion.Steps = steps
	}
	if dryRun {
		return nil
	}
	if len(suggestion.Steps) == 0 {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Plan has no executable steps.")
		}
		return nil
	}
	for i, step := range suggestion.Steps {
		r.logger.Printf("Executing step %d/%d: %s", i+1, len(suggestion.Steps), step)
		stopIndicator := r.startLLMIndicatorIfAllowed("assist")
		result, err := r.getAssistSuggestion(step, "execute-step")
		stopIndicator()
		if err != nil {
			return err
		}
		if err := r.executeAssistSuggestion(result, false); err != nil {
			if r.handleAssistCommandFailure(step, result, err) {
				return nil
			}
			return err
		}
		if result.Type == "question" {
			r.logger.Printf("Plan paused for user input. Continue after answering.")
			break
		}
	}
	return nil
}

func (r *Runner) handleAssistCommandFailure(goal string, suggestion assist.Suggestion, err error) bool {
	if err == nil {
		return false
	}
	var cmdErr commandError
	if !errors.As(err, &cmdErr) {
		cmdErr = commandError{
			Result: exec.CommandResult{
				Command: suggestion.Command,
				Args:    suggestion.Args,
			},
			Err: err,
		}
	}
	summary := summarizeCommandFailure(cmdErr)
	if summary != "" {
		r.logger.Printf("Command failed: %s", summary)
	} else {
		r.logger.Printf("Command failed: %v", cmdErr.Err)
	}
	if hint := assistFailureHint(suggestion, cmdErr); hint != "" {
		r.logger.Printf("Hint: %s", hint)
	}
	if r.tryAssistRecovery(suggestion, cmdErr) {
		return true
	}
	return r.suggestAssistRecovery(goal, suggestion, cmdErr)
}

func summarizeCommandFailure(cmdErr commandError) string {
	parts := []string{}
	if cmdErr.Err != nil {
		parts = append(parts, cmdErr.Err.Error())
	}
	if output := firstLines(cmdErr.Result.Output, 2); output != "" {
		parts = append(parts, output)
	}
	return strings.Join(parts, " | ")
}

func (r *Runner) suggestAssistRecovery(goal string, suggestion assist.Suggestion, cmdErr commandError) bool {
	if !r.llmAllowed() {
		r.logger.Printf("No recovery suggestion (LLM unavailable).")
		return true
	}
	recoveryGoal := buildRecoveryGoal(goal, suggestion, cmdErr)
	label := "recover"
	stopIndicator := r.startLLMIndicatorIfAllowed(label)
	recovery, err := r.getAssistSuggestion(recoveryGoal, "recover")
	stopIndicator()
	if err != nil {
		r.logger.Printf("Recovery suggestion failed: %v", err)
		return true
	}
	if recovery.Type == "noop" {
		r.logger.Printf("No recovery suggestion provided.")
		return true
	}
	if recovery.Type == "question" {
		fmt.Println(recovery.Question)
		r.appendConversation("Assistant", recovery.Question)
		if strings.TrimSpace(goal) != "" {
			r.pendingAssistGoal = goal
		}
		return true
	}
	if recovery.Type == "plan" {
		_ = r.handlePlanSuggestion(recovery, true)
		return true
	}
	if recovery.Type == "command" {
		r.logger.Printf("Recovery suggestion: %s %s", recovery.Command, strings.Join(recovery.Args, " "))
		if recovery.Summary != "" {
			r.logger.Printf("Recovery summary: %s", recovery.Summary)
		}
		if recovery.Risk != "" {
			r.logger.Printf("Recovery risk: %s", recovery.Risk)
		}
		if err := r.executeAssistSuggestion(recovery, false); err != nil {
			r.logger.Printf("Recovery attempt failed: %v", err)
		}
		return true
	}
	r.logger.Printf("Recovery suggestion returned unknown type: %s", recovery.Type)
	return true
}

func (r *Runner) maybeSuggestNextSteps(goal string, lastSuggestion assist.Suggestion) {
	if !r.llmAllowed() {
		return
	}
	if strings.TrimSpace(goal) == "" && lastSuggestion.Summary == "" {
		return
	}
	nextGoal := buildNextStepsGoal(goal, lastSuggestion)
	stopIndicator := r.startLLMIndicatorIfAllowed("next steps")
	next, err := r.getAssistSuggestion(nextGoal, "next-steps")
	stopIndicator()
	if err != nil {
		r.logger.Printf("Next-step suggestion failed: %v", err)
		return
	}
	switch next.Type {
	case "question":
		if next.Question != "" {
			fmt.Println(next.Question)
			r.appendConversation("Assistant", next.Question)
			if strings.TrimSpace(goal) != "" {
				r.pendingAssistGoal = goal
			}
		}
	case "plan":
		steps := next.Steps
		if len(steps) == 0 && next.Plan != "" {
			fmt.Println("Possible next steps:")
			fmt.Println(next.Plan)
			r.appendConversation("Assistant", "Possible next steps: "+next.Plan)
			return
		}
		if len(steps) > 0 {
			fmt.Println("Possible next steps:")
			for i, step := range steps {
				fmt.Printf("%d) %s\n", i+1, step)
			}
			r.appendConversation("Assistant", "Possible next steps: "+strings.Join(steps, " | "))
		}
	case "command":
		cmdLine := strings.TrimSpace(strings.Join(append([]string{next.Command}, next.Args...), " "))
		if cmdLine != "" {
			fmt.Printf("Suggested next command: %s\n", cmdLine)
		}
		if next.Summary != "" {
			fmt.Printf("Why: %s\n", next.Summary)
		}
		r.appendConversation("Assistant", fmt.Sprintf("Suggested next command: %s", cmdLine))
	default:
		if r.cfg.UI.Verbose {
			r.logger.Printf("No next-step suggestion.")
		}
	}
}

func (r *Runner) maybeEmitGoalSummary(goal string, dryRun bool) {
	if dryRun {
		return
	}
	goal = strings.TrimSpace(goal)
	if !isSummaryIntent(goal) {
		return
	}
	if strings.TrimSpace(r.lastActionLogPath) == "" {
		return
	}
	if err := r.summarizeFromLatestArtifact(goal); err != nil && r.cfg.UI.Verbose {
		r.logger.Printf("Summary generation failed: %v", err)
	}
}

func (r *Runner) summarizeFromLatestArtifact(goal string) error {
	if strings.TrimSpace(goal) == "" || strings.TrimSpace(r.lastActionLogPath) == "" {
		return nil
	}
	if !r.llmAllowed() {
		return nil
	}
	artifactPath := r.lastActionLogPath
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return fmt.Errorf("read latest artifact: %w", err)
	}
	const maxArtifactBytes = 16000
	if len(data) > maxArtifactBytes {
		data = data[len(data)-maxArtifactBytes:]
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	artifacts, _ := memory.EnsureArtifacts(sessionDir)
	contextSummary := readFileTrimmed(artifacts.SummaryPath)
	contextFacts := readFileTrimmed(artifacts.FactsPath)
	contextFocus := readFileTrimmed(artifacts.FocusPath)
	prompt := strings.Builder{}
	prompt.WriteString("Goal:\n")
	prompt.WriteString(goal + "\n\n")
	prompt.WriteString("Latest action artifact path:\n")
	prompt.WriteString(artifactPath + "\n\n")
	prompt.WriteString("Latest action artifact content:\n")
	prompt.WriteString(string(data) + "\n\n")
	if contextSummary != "" {
		prompt.WriteString("Session summary:\n" + contextSummary + "\n\n")
	}
	if contextFacts != "" {
		prompt.WriteString("Known facts:\n" + contextFacts + "\n\n")
	}
	if contextFocus != "" {
		prompt.WriteString("Task foundation:\n" + contextFocus + "\n\n")
	}
	prompt.WriteString("Provide: 1) concise summary, 2) known findings, 3) unknown/missing data, 4) next 2-3 concrete steps.")

	client := llm.NewLMStudioClient(r.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("summary")
	defer stopIndicator()
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: 0.2,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: "You are BirdHackBot. Summarize from provided artifact/content only. Do not ask user to paste files if artifact content is already present.",
			},
			{
				Role:    "user",
				Content: prompt.String(),
			},
		},
	})
	if err != nil {
		r.recordLLMFailure(err)
		return err
	}
	r.recordLLMSuccess()
	fmt.Println(resp.Content)
	r.appendConversation("Assistant", resp.Content)
	return nil
}

func assistFailureHint(suggestion assist.Suggestion, cmdErr commandError) string {
	command := strings.ToLower(strings.TrimSpace(suggestion.Command))
	outputLower := strings.ToLower(cmdErr.Result.Output)
	errLower := ""
	if cmdErr.Err != nil {
		errLower = strings.ToLower(cmdErr.Err.Error())
	}
	if command == "whois" {
		if strings.Contains(outputLower, "no match") || strings.Contains(outputLower, "not found") {
			return "WHOIS typically only supports root domains (e.g., systemverification.com), not subdomains."
		}
	}
	if strings.Contains(errLower, "executable file not found") || strings.Contains(errLower, "not found") {
		return "Tool not available in PATH. Install it or update your tool inventory."
	}
	return ""
}

func (r *Runner) tryAssistRecovery(suggestion assist.Suggestion, cmdErr commandError) bool {
	command := strings.ToLower(strings.TrimSpace(suggestion.Command))
	if command != "whois" {
		return false
	}
	if len(cmdErr.Result.Args) == 0 {
		return false
	}
	original := cmdErr.Result.Args[0]
	alt, ok := normalizeWhoisTarget(original)
	if !ok || strings.EqualFold(alt, original) {
		return false
	}
	outputLower := strings.ToLower(cmdErr.Result.Output)
	if outputLower != "" && !strings.Contains(outputLower, "no match") && !strings.Contains(outputLower, "not found") {
		return false
	}
	r.logger.Printf("Retrying whois with root domain: %s", alt)
	if retryErr := r.handleRun([]string{"whois", alt}); retryErr != nil {
		r.logger.Printf("Retry failed: %v", retryErr)
	}
	return true
}

func buildRecoveryGoal(goal string, suggestion assist.Suggestion, cmdErr commandError) string {
	builder := strings.Builder{}
	if goal != "" {
		builder.WriteString("Original goal: " + goal + "\n")
	}
	builder.WriteString("Previous command failed.\n")
	cmdLine := strings.TrimSpace(strings.Join(append([]string{suggestion.Command}, suggestion.Args...), " "))
	if cmdLine != "" {
		builder.WriteString("Command: " + cmdLine + "\n")
	}
	if cmdErr.Err != nil {
		builder.WriteString("Error: " + cmdErr.Err.Error() + "\n")
	}
	if output := firstLines(cmdErr.Result.Output, 3); output != "" {
		builder.WriteString("Output: " + output + "\n")
	}
	builder.WriteString("Provide a recovery suggestion or alternative next step.")
	return builder.String()
}

func buildNextStepsGoal(goal string, suggestion assist.Suggestion) string {
	builder := strings.Builder{}
	if goal != "" {
		builder.WriteString("Original goal: " + goal + "\n")
	}
	if suggestion.Summary != "" {
		builder.WriteString("Last action summary: " + suggestion.Summary + "\n")
	}
	cmdLine := strings.TrimSpace(strings.Join(append([]string{suggestion.Command}, suggestion.Args...), " "))
	if cmdLine != "" {
		builder.WriteString("Last command: " + cmdLine + "\n")
	}
	builder.WriteString("Suggest 1-3 concise next steps or a clarifying question.")
	return builder.String()
}

func normalizeWhoisTarget(target string) (string, bool) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	lower = strings.TrimPrefix(lower, "http://")
	lower = strings.TrimPrefix(lower, "https://")
	lower = strings.SplitN(lower, "/", 2)[0]
	lower = strings.TrimSuffix(lower, ".")
	if strings.HasPrefix(lower, "www.") {
		lower = strings.TrimPrefix(lower, "www.")
	}
	if lower == "" {
		return "", false
	}
	return lower, true
}

func firstLines(text string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	out := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= maxLines {
			break
		}
	}
	return strings.Join(out, " / ")
}

func (r *Runner) resetAssistCommandLoop() {
	r.lastAssistCmdKey = ""
	r.lastAssistCmdSeen = 0
}

func (r *Runner) resetAssistLoopState() {
	r.resetAssistCommandLoop()
	r.lastAssistQuestion = ""
	r.lastAssistQSeen = 0
}

func (r *Runner) guardAssistCommandLoop(command string, args []string) error {
	key := strings.ToLower(strings.TrimSpace(command)) + "\x1f" + strings.Join(args, "\x1f")
	if key == r.lastAssistCmdKey {
		r.lastAssistCmdSeen++
	} else {
		r.lastAssistCmdKey = key
		r.lastAssistCmdSeen = 1
	}
	const maxSameCommandInRow = 2
	if r.lastAssistCmdSeen > maxSameCommandInRow {
		return fmt.Errorf("assistant loop guard: repeated command blocked: %s %s", command, strings.Join(args, " "))
	}
	r.lastAssistQuestion = ""
	r.lastAssistQSeen = 0
	return nil
}

func (r *Runner) guardAssistQuestionLoop(question string) error {
	key := strings.TrimSpace(strings.ToLower(question))
	if key == "" {
		return nil
	}
	if key == r.lastAssistQuestion {
		r.lastAssistQSeen++
	} else {
		r.lastAssistQuestion = key
		r.lastAssistQSeen = 1
	}
	const maxSameQuestionInRow = 2
	if r.lastAssistQSeen > maxSameQuestionInRow {
		return fmt.Errorf("assistant loop guard: repeated question blocked")
	}
	r.resetAssistCommandLoop()
	return nil
}

func sanitizeBrowseArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("assistant returned browse without url")
	}
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		filtered = append(filtered, arg)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("assistant returned browse without url")
	}
	return []string{filtered[0]}, nil
}

func (r *Runner) enrichAssistGoal(goal, mode string) string {
	if mode != "recover" && mode != "follow-up" && mode != "next-steps" {
		return goal
	}
	path := strings.TrimSpace(r.lastActionLogPath)
	if path == "" {
		return goal
	}
	if _, err := os.Stat(path); err != nil {
		return goal
	}
	builder := strings.Builder{}
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	builder.WriteString("Context: latest action artifact: ")
	builder.WriteString(path)
	builder.WriteString(". Prefer analyzing this local artifact before repeating the same action.")
	return builder.String()
}

func (r *Runner) recordActionArtifact(logPath string) {
	path := strings.TrimSpace(logPath)
	if path == "" {
		return
	}
	r.lastActionLogPath = path
}

func (r *Runner) clearActionContext() {
	r.lastActionLogPath = ""
	r.lastBrowseLogPath = ""
}

func (r *Runner) updateKnownTargetFromText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if url := extractFirstURL(text); url != "" {
		if normalized, err := normalizeURL(url); err == nil {
			if parsed, err := neturl.Parse(normalized); err == nil && parsed.Host != "" {
				r.lastKnownTarget = parsed.Hostname()
				return
			}
		}
	}
	token := extractHostLikeToken(text)
	if token != "" {
		r.lastKnownTarget = token
	}
}

func (r *Runner) bestKnownTarget() string {
	target := strings.TrimSpace(r.lastKnownTarget)
	if target == "" {
		return ""
	}
	if strings.Contains(target, "://") {
		parsed, err := neturl.Parse(target)
		if err == nil && parsed.Hostname() != "" {
			return parsed.Hostname()
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(target, "."))
}

func (r *Runner) appendConversation(role, content string) {
	content = strings.TrimSpace(content)
	if role == "" || content == "" {
		return
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return
	}
	r.appendChatHistory(artifacts.ChatPath, role, content)
	r.maybeAutoSummarizeChat(sessionDir, artifacts.ChatPath)
}

func (r *Runner) updateTaskFoundation(goal string) {
	goal = collapseWhitespace(strings.TrimSpace(goal))
	if goal == "" {
		return
	}
	if len(goal) > 240 {
		goal = goal[:240]
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return
	}
	existing, err := memory.ReadBullets(artifacts.FocusPath)
	if err != nil {
		existing = nil
	}
	items := []string{goal}
	for _, item := range existing {
		item = collapseWhitespace(strings.TrimSpace(item))
		if item == "" || strings.EqualFold(item, goal) || strings.EqualFold(item, "Not set.") {
			continue
		}
		items = append(items, item)
		if len(items) >= 12 {
			break
		}
	}
	_ = memory.WriteFocus(artifacts.FocusPath, items)
}

func (r *Runner) maybeAutoSummarizeChat(sessionDir, chatPath string) {
	if chatPath == "" {
		return
	}
	manager := r.memoryManager(sessionDir)
	state, err := manager.RecordLog(chatPath)
	if err != nil {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Context tracking failed: %v", err)
		}
		return
	}
	if !manager.ShouldSummarize(state) {
		return
	}
	if r.cfg.Permissions.Level == "readonly" {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Auto-summarize skipped (readonly)")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("summarize")
	defer stopIndicator()
	if err := manager.Summarize(ctx, r.summaryGenerator(), "chat"); err != nil {
		r.logger.Printf("Auto-summarize failed: %v", err)
		return
	}
	if r.cfg.UI.Verbose {
		r.logger.Printf("Auto-summary updated")
	}
}

func isPlaceholderCommand(cmd string) bool {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	switch cmd {
	case "scan", "recon", "enumerate", "probe", "test":
		return true
	default:
		return false
	}
}

func isSummaryIntent(goal string) bool {
	if goal == "" {
		return false
	}
	lower := strings.ToLower(goal)
	hints := []string{"summary", "summarize", "what did you find", "findings", "what is it about", "tell me what", "report"}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func shouldStartBaselineScan(answer string) bool {
	normalized := strings.ToLower(strings.TrimSpace(answer))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "scan") && (strings.Contains(normalized, "start") || strings.Contains(normalized, "first") || strings.HasPrefix(normalized, "yes")) {
		return true
	}
	if strings.Contains(normalized, "non intrusive") && strings.Contains(normalized, "start") {
		return true
	}
	return false
}

func extractHostLikeToken(text string) string {
	candidates := strings.Fields(strings.ToLower(text))
	for _, token := range candidates {
		token = strings.Trim(token, "\"'()[]{}<>.,;:")
		if token == "" {
			continue
		}
		if strings.Contains(token, "/") || strings.Contains(token, ":") {
			continue
		}
		if strings.Count(token, ".") >= 1 && !strings.HasPrefix(token, ".") && !strings.HasSuffix(token, ".") {
			return token
		}
	}
	return ""
}

func isBenignNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	var cmdErr commandError
	if !errors.As(err, &cmdErr) {
		return false
	}
	if cmdErr.Err == nil || !strings.Contains(strings.ToLower(cmdErr.Err.Error()), "exit status 1") {
		return false
	}
	command := strings.ToLower(strings.TrimSpace(cmdErr.Result.Command))
	switch command {
	case "grep", "rg":
		return strings.TrimSpace(cmdErr.Result.Output) == ""
	case "bash", "sh":
		if len(cmdErr.Result.Args) == 0 {
			return false
		}
		joined := strings.ToLower(strings.Join(cmdErr.Result.Args, " "))
		return strings.Contains(joined, "grep") && strings.TrimSpace(cmdErr.Result.Output) == ""
	default:
		return false
	}
}

func looksLikeChat(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return false
	}
	if looksLikeAction(trimmed) {
		return false
	}
	if strings.Contains(trimmed, "?") {
		return true
	}
	if hasPrefixOneOf(trimmed, "hello", "hi", "hey", "good morning", "good afternoon", "good evening") {
		return true
	}
	if hasPrefixOneOf(trimmed, "my name is", "i am", "i'm") {
		return true
	}
	if hasPrefixOneOf(trimmed, "who ", "what ", "where ", "why ", "how ", "can you", "could you", "would you", "tell me", "explain", "describe") {
		return true
	}
	return true
}

func looksLikeAction(text string) bool {
	if hasURLHint(text) {
		return true
	}
	if looksLikeFileQuery(text) {
		return true
	}
	verbs := map[string]struct{}{
		"scan": {}, "enumerate": {}, "list": {}, "show": {}, "find": {}, "run": {}, "check": {}, "exploit": {},
		"test": {}, "probe": {}, "search": {}, "ping": {}, "nmap": {}, "curl": {}, "msf": {}, "msfconsole": {},
		"netstat": {}, "ls": {}, "whoami": {}, "cat": {}, "dir": {}, "open": {}, "dump": {}, "inspect": {}, "analyze": {},
	}
	for _, token := range splitTokens(text) {
		if _, ok := verbs[token]; ok {
			return true
		}
	}
	return false
}

func hasPrefixOneOf(text string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func splitTokens(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= '0' && r <= '9' {
			return false
		}
		return true
	})
}

func looksLikeFileQuery(text string) bool {
	lower := strings.ToLower(text)
	if !hasFileHint(lower) {
		return false
	}
	if strings.Contains(lower, "?") {
		return true
	}
	if hasPrefixOneOf(lower, "open", "read", "show", "view", "display", "cat", "print") {
		return true
	}
	if strings.Contains(lower, "inside") || strings.Contains(lower, "contents") || strings.Contains(lower, "content") {
		return true
	}
	if strings.Contains(lower, "what is in") || strings.Contains(lower, "what's in") {
		return true
	}
	return false
}

func hasFileHint(text string) bool {
	if strings.Contains(text, "readme") {
		return true
	}
	if strings.Contains(text, "/") || strings.Contains(text, "\\") {
		return true
	}
	extensions := []string{".md", ".txt", ".log", ".json", ".yaml", ".yml", ".toml", ".ini", ".conf", ".cfg", ".go", ".py", ".sh", ".js", ".ts", ".zip", ".7z"}
	for _, ext := range extensions {
		if strings.Contains(text, ext) {
			return true
		}
	}
	return false
}

func hasURLHint(text string) bool {
	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		return true
	}
	for _, token := range strings.Fields(text) {
		clean := strings.Trim(token, " \t\r\n\"'()[]{}<>.,;:")
		if strings.Count(clean, ".") >= 1 && len(clean) >= 4 {
			if strings.HasPrefix(clean, ".") || strings.HasSuffix(clean, ".") {
				continue
			}
			return true
		}
	}
	return false
}

func extractFirstURL(text string) string {
	if text == "" {
		return ""
	}
	if match := findURLWithScheme(text); match != "" {
		return match
	}
	for _, token := range strings.Fields(text) {
		clean := strings.Trim(token, " \t\r\n\"'()[]{}<>.,;:")
		if clean == "" {
			continue
		}
		if strings.Contains(clean, "://") {
			return clean
		}
		if strings.HasPrefix(strings.ToLower(clean), "www.") || strings.Count(clean, ".") >= 1 {
			if strings.HasPrefix(clean, ".") || strings.HasSuffix(clean, ".") {
				continue
			}
			return clean
		}
	}
	return ""
}

func findURLWithScheme(text string) string {
	start := strings.Index(text, "http://")
	if start == -1 {
		start = strings.Index(text, "https://")
	}
	if start == -1 {
		return ""
	}
	rest := text[start:]
	end := len(rest)
	for i, r := range rest {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			end = i
			break
		}
	}
	candidate := strings.Trim(rest[:end], "\"'()[]{}<>.,;:")
	return candidate
}

func shouldAutoBrowse(text string) bool {
	url := extractFirstURL(text)
	if url == "" {
		return false
	}
	lower := strings.ToLower(text)
	blockers := []string{"scan", "enumerate", "ffuf", "gobuster", "dirsearch", "nmap", "nikto", "nuclei", "exploit", "attack"}
	for _, word := range blockers {
		if strings.Contains(lower, word) {
			return false
		}
	}
	if strings.TrimSpace(lower) == strings.ToLower(url) || strings.TrimSpace(lower) == "www."+strings.TrimPrefix(strings.ToLower(url), "www.") {
		return true
	}
	infoHints := []string{"what", "tell me", "about", "overview", "summarize", "whois", "info", "information", "website", "site", "page"}
	for _, hint := range infoHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func (r *Runner) readLine(prompt string) (string, error) {
	if prompt != "" && r.isTTY() && strings.HasPrefix(prompt, "BirdHackBot") {
		return r.readLineInteractive(prompt)
	}
	if prompt != "" {
		fmt.Print(prompt)
	}
	line, err := r.reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), err
}

func (r *Runner) readLineInteractive(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		if prompt != "" {
			fmt.Print(prompt)
		}
		line, err := r.reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		return strings.TrimSpace(line), err
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		if prompt != "" {
			fmt.Print(prompt)
		}
		line, readErr := r.reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return "", readErr
		}
		return strings.TrimSpace(line), readErr
	}
	defer func() {
		_ = term.Restore(fd, state)
	}()

	buf := make([]byte, 0, 128)
	recordHistory := strings.HasPrefix(prompt, "BirdHackBot")
	r.historyIndex = -1
	r.historyScratch = ""

	r.redrawInputLine(prompt, buf)

	for {
		b, readErr := r.reader.ReadByte()
		if readErr != nil {
			return "", readErr
		}
		switch b {
		case '\r', '\n':
			fmt.Print("\r\n")
			r.inputRenderLines = 0
			line := strings.TrimSpace(string(buf))
			if recordHistory && line != "" {
				if len(r.history) == 0 || r.history[len(r.history)-1] != line {
					r.history = append(r.history, line)
				}
			}
			r.historyIndex = -1
			r.historyScratch = ""
			return line, nil
		case 0x03:
			fmt.Print("\r\n")
			r.inputRenderLines = 0
			r.historyIndex = -1
			r.historyScratch = ""
			return "", io.EOF
		case 0x04:
			if len(buf) == 0 {
				fmt.Print("\r\n")
				r.inputRenderLines = 0
				r.historyIndex = -1
				r.historyScratch = ""
				return "", io.EOF
			}
		case 0x7f, 0x08:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				r.redrawInputLine(prompt, buf)
			}
			continue
		case 0x1b:
			seq1, seqErr := r.reader.ReadByte()
			if seqErr != nil {
				continue
			}
			if seq1 != '[' {
				continue
			}
			seq2, seqErr := r.reader.ReadByte()
			if seqErr != nil {
				continue
			}
			switch seq2 {
			case 'A':
				if len(r.history) == 0 {
					continue
				}
				if r.historyIndex == -1 {
					r.historyScratch = string(buf)
					r.historyIndex = len(r.history) - 1
				} else if r.historyIndex > 0 {
					r.historyIndex--
				}
				buf = []byte(r.history[r.historyIndex])
				r.redrawInputLine(prompt, buf)
			case 'B':
				if r.historyIndex == -1 {
					continue
				}
				if r.historyIndex < len(r.history)-1 {
					r.historyIndex++
					buf = []byte(r.history[r.historyIndex])
				} else {
					r.historyIndex = -1
					buf = []byte(r.historyScratch)
					r.historyScratch = ""
				}
				r.redrawInputLine(prompt, buf)
			}
			continue
		default:
			if b >= 32 && b != 127 {
				buf = append(buf, b)
				r.redrawInputLine(prompt, buf)
			}
		}
	}
}

func (r *Runner) redrawInputLine(prompt string, buf []byte) {
	r.clearInputRender()
	fmt.Printf("%s%s%s%s", inputLineStyleStart, prompt, string(buf), inputLineStyleReset)
	r.inputRenderLines = visualLineCount(prompt+string(buf), terminalWidth())
}

func (r *Runner) clearInputRender() {
	if r.inputRenderLines <= 0 {
		return
	}
	for i := 0; i < r.inputRenderLines; i++ {
		fmt.Print("\r\x1b[2K")
		if i < r.inputRenderLines-1 {
			fmt.Print("\x1b[1A")
		}
	}
	fmt.Print("\r")
}

func terminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 80
	}
	return width
}

func visualLineCount(text string, width int) int {
	if width <= 0 {
		width = 80
	}
	runes := len([]rune(text))
	if runes <= 0 {
		return 1
	}
	lines := runes / width
	if runes%width != 0 {
		lines++
	}
	if lines <= 0 {
		return 1
	}
	return lines
}

func (r *Runner) confirm(prompt string) (bool, error) {
	for {
		line, err := r.readLine(fmt.Sprintf("%s [y/N]: ", prompt))
		if err != nil && err != io.EOF {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" || answer == "n" || answer == "no" {
			return false, nil
		}
		if answer == "y" || answer == "yes" {
			return true, nil
		}
		if err == io.EOF {
			return false, nil
		}
	}
}

func (r *Runner) prompt() string {
	if r.currentTask == "" {
		if r.currentMode != "" {
			return fmt.Sprintf("BirdHackBot[%s]> ", r.currentMode)
		}
		return "BirdHackBot> "
	}
	elapsed := formatElapsed(time.Since(r.currentTaskStart))
	return fmt.Sprintf("BirdHackBot[%s %s]> ", r.currentTask, elapsed)
}

func (r *Runner) setTask(task string) {
	r.currentTask = task
	r.currentTaskStart = time.Now()
	r.logger.Printf("Task: %s", task)
}

func (r *Runner) clearTask() {
	r.currentTask = ""
	r.currentTaskStart = time.Time{}
}

func (r *Runner) setMode(mode string) {
	r.currentMode = mode
}

func (r *Runner) clearMode() {
	r.currentMode = ""
}

func formatElapsed(d time.Duration) string {
	totalSeconds := int(d.Seconds())
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

type commandError struct {
	Result exec.CommandResult
	Err    error
}

func (e commandError) Error() string {
	if e.Err == nil {
		return "command failed"
	}
	return e.Err.Error()
}

func (e commandError) Unwrap() error {
	return e.Err
}

func (r *Runner) startLLMIndicator(label string) func() {
	r.setLLMStatus(label)
	if !r.isTTY() {
		return func() {
			r.clearLLMStatus()
		}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		timer := time.NewTimer(llmIndicatorDelay)
		defer timer.Stop()
		select {
		case <-stop:
			return
		case <-timer.C:
		}
		frames := []string{"-", "\\", "|", "/"}
		idx := 0
		start := time.Now()
		ticker := time.NewTicker(llmIndicatorInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				fmt.Print("\r\x1b[2K")
				return
			case <-ticker.C:
				elapsed := formatElapsed(time.Since(start))
				fmt.Printf("\rLLM %s %s (%s)", label, frames[idx], elapsed)
				idx = (idx + 1) % len(frames)
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(stop)
			<-done
			r.clearLLMStatus()
		})
	}
}

func (r *Runner) startLLMIndicatorIfAllowed(label string) func() {
	if !r.llmAllowed() {
		return func() {}
	}
	return r.startLLMIndicator(label)
}

func (r *Runner) setLLMStatus(label string) {
	if label == "" {
		label = "thinking"
	}
	r.llmMu.Lock()
	r.llmInFlight = true
	r.llmLabel = label
	r.llmStarted = time.Now()
	r.llmMu.Unlock()
}

func (r *Runner) clearLLMStatus() {
	r.llmMu.Lock()
	r.llmInFlight = false
	r.llmLabel = ""
	r.llmStarted = time.Time{}
	r.llmMu.Unlock()
}

func (r *Runner) llmStatus() (bool, string, time.Time) {
	r.llmMu.Lock()
	defer r.llmMu.Unlock()
	return r.llmInFlight, r.llmLabel, r.llmStarted
}

func (r *Runner) isTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
