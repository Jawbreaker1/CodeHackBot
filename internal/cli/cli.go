package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
	"github.com/Jawbreaker1/CodeHackBot/internal/report"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

const (
	inputLineStyleStart = "\x1b[48;5;236m\x1b[38;5;252m"
	inputLineStyleReset = "\x1b[0m"
)

type Runner struct {
	cfg               config.Config
	sessionID         string
	logger            *log.Logger
	reader            *bufio.Reader
	defaultConfigPath string
	profilePath       string
	stopOnce          sync.Once
	currentTask       string
	currentTaskStart  time.Time
	llmGuard          llm.Guard
}

func NewRunner(cfg config.Config, sessionID, defaultConfigPath, profilePath string) *Runner {
	return &Runner{
		cfg:               cfg,
		sessionID:         sessionID,
		logger:            log.New(os.Stdout, fmt.Sprintf("[session:%s] ", sessionID), 0),
		reader:            bufio.NewReader(os.Stdin),
		defaultConfigPath: defaultConfigPath,
		profilePath:       profilePath,
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
		if looksLikeChat(line) {
			if err := r.handleAsk(line); err != nil {
				r.logger.Printf("Ask error: %v", err)
			}
		} else {
			if assistErr := r.handleAssistGoal(line, false); assistErr != nil {
				r.logger.Printf("Assist error: %v", assistErr)
			}
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

	switch cmd {
	case "help":
		r.printHelp()
	case "init":
		return r.handleInit(args)
	case "permissions":
		return r.handlePermissions(args)
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
	case "assist":
		return r.handleAssist(args)
	case "script":
		return r.handleScript(args)
	case "clean":
		return r.handleClean(args)
	case "ask":
		return r.handleAsk(strings.Join(args, " "))
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
	if r.currentTask == "" {
		r.logger.Printf("Status: idle")
		return
	}
	elapsed := formatElapsed(time.Since(r.currentTaskStart))
	r.logger.Printf("Status: running %s (%s)", r.currentTask, elapsed)
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

	if len(args) > 0 {
		mode := strings.ToLower(args[0])
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
	planner := r.planGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
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
	planner := r.planGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
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
	client := llm.NewLMStudioClient(r.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: 0.2,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: "You are BirdHackBot, a security testing assistant. Answer clearly and concisely. If clarification is needed, ask follow-up questions. Stay within authorized scope.",
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
	r.logger.Printf("Assistant response:")
	fmt.Println(resp.Content)
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
	runner := exec.Runner{
		Permissions:      exec.PermissionLevel(r.cfg.Permissions.Level),
		RequireApproval:  false,
		LogDir:           filepath.Join(r.cfg.Session.LogDir, r.sessionID, "logs"),
		Timeout:          timeout,
		Reader:           r.reader,
		ScopeNetworks:    r.cfg.Scope.Networks,
		ScopeTargets:     r.cfg.Scope.Targets,
		ScopeDenyTargets: r.cfg.Scope.DenyTargets,
		LiveWriter:       r.liveWriter(),
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
		}
		return err
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

	execRunner := exec.Runner{
		Permissions:      exec.PermissionLevel(r.cfg.Permissions.Level),
		RequireApproval:  false,
		LogDir:           filepath.Join(r.cfg.Session.LogDir, r.sessionID, "logs"),
		Timeout:          2 * time.Minute,
		Reader:           r.reader,
		ScopeNetworks:    r.cfg.Scope.Networks,
		ScopeTargets:     r.cfg.Scope.Targets,
		ScopeDenyTargets: r.cfg.Scope.DenyTargets,
		LiveWriter:       r.liveWriter(),
	}
	cmdArgs := []string{"-q", "-x", command}
	result, err := execRunner.RunCommandWithContext(ctx, "msfconsole", cmdArgs...)
	wasCanceled := errors.Is(err, context.Canceled)
	if stopInterrupt != nil {
		stopInterrupt()
	}
	if result.LogPath != "" {
		r.logger.Printf("Log saved: %s", result.LogPath)
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
	r.logger.Printf("Commands: /init /permissions /context [/show] /ledger /status /plan /next /assist /script /clean /ask /summarize /run /msf /report /resume /stop /exit")
	r.logger.Printf("Example: /permissions readonly")
	r.logger.Printf("Plain text input routes to /assist; use /ask for chat-only.")
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

	return plan.Input{
		SessionID:  r.sessionID,
		Scope:      r.cfg.Scope.Networks,
		Targets:    r.cfg.Scope.Targets,
		Summary:    summaryText,
		KnownFacts: facts,
		Inventory:  inventory,
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

func (r *Runner) assistInput(sessionDir, goal string) (assist.Input, error) {
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return assist.Input{}, err
	}
	summaryText := readFileTrimmed(artifacts.SummaryPath)
	facts, _ := memory.ReadBullets(artifacts.FactsPath)
	planPath := filepath.Join(sessionDir, r.cfg.Session.PlanFilename)
	inventoryPath := filepath.Join(sessionDir, r.cfg.Session.InventoryFilename)
	return assist.Input{
		SessionID:  r.sessionID,
		Scope:      r.cfg.Scope.Networks,
		Targets:    r.cfg.Scope.Targets,
		Summary:    summaryText,
		KnownFacts: facts,
		Plan:       readFileTrimmed(planPath),
		Inventory:  readFileTrimmed(inventoryPath),
		Goal:       strings.TrimSpace(goal),
	}, nil
}

func (r *Runner) buildAskPrompt(sessionDir, question string) string {
	artifacts, _ := memory.EnsureArtifacts(sessionDir)
	summary := readFileTrimmed(artifacts.SummaryPath)
	facts := readFileTrimmed(artifacts.FactsPath)
	focus := readFileTrimmed(artifacts.FocusPath)
	planPath := filepath.Join(sessionDir, r.cfg.Session.PlanFilename)
	inventoryPath := filepath.Join(sessionDir, r.cfg.Session.InventoryFilename)

	builder := strings.Builder{}
	builder.WriteString("User question: " + question + "\n")
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

func (r *Runner) getAssistSuggestion(goal string) (assist.Suggestion, error) {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return assist.Suggestion{}, err
	}
	input, err := r.assistInput(sessionDir, goal)
	if err != nil {
		return assist.Suggestion{}, err
	}
	assistant := r.assistGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return assistant.Suggest(ctx, input)
}

func (r *Runner) handleAssistGoal(goal string, dryRun bool) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: assist not permitted")
	}
	suggestion, err := r.getAssistSuggestion(goal)
	if err != nil {
		return err
	}
	if suggestion.Type == "noop" && strings.TrimSpace(goal) != "" {
		r.logger.Printf("No actionable suggestion; answering via /ask.")
		return r.handleAsk(goal)
	}
	return r.executeAssistSuggestion(suggestion, dryRun)
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
		r.logger.Printf("Assistant question: %s", suggestion.Question)
		if suggestion.Summary != "" {
			r.logger.Printf("Summary: %s", suggestion.Summary)
		}
		return nil
	case "noop":
		r.logger.Printf("Assistant has no suggestion")
		return nil
	case "command":
		if suggestion.Command == "" {
			return fmt.Errorf("assistant returned empty command")
		}
	default:
		return fmt.Errorf("assistant returned unknown type: %s", suggestion.Type)
	}

	r.logger.Printf("Suggested command: %s %s", suggestion.Command, strings.Join(suggestion.Args, " "))
	if suggestion.Summary != "" {
		r.logger.Printf("Summary: %s", suggestion.Summary)
	}
	if suggestion.Risk != "" {
		r.logger.Printf("Risk: %s", suggestion.Risk)
	}
	if dryRun {
		return nil
	}
	args := append([]string{suggestion.Command}, suggestion.Args...)
	return r.handleRun(args)
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

func looksLikeChat(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
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
	if looksLikeAction(trimmed) {
		return false
	}
	return true
}

func looksLikeAction(text string) bool {
	verbs := []string{
		"scan", "enumerate", "list", "show", "find", "run", "check", "exploit",
		"test", "probe", "search", "ping", "nmap", "curl", "msf", "msfconsole",
		"netstat", "ls", "whoami", "cat", "dir", "open", "dump", "inspect", "analyze",
	}
	for _, verb := range verbs {
		if text == verb || strings.HasPrefix(text, verb+" ") {
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

func (r *Runner) readLine(prompt string) (string, error) {
	useStyle := prompt != "" && r.isTTY()
	if prompt != "" {
		if useStyle {
			fmt.Print(inputLineStyleStart)
		}
		fmt.Print(prompt)
	}
	line, err := r.reader.ReadString('\n')
	if useStyle {
		fmt.Print(inputLineStyleReset)
	}
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), err
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

func (r *Runner) isTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
