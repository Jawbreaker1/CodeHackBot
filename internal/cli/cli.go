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
	"strings"
	"sync"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
	"github.com/Jawbreaker1/CodeHackBot/internal/msf"
	"github.com/Jawbreaker1/CodeHackBot/internal/report"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
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
}

func NewRunner(cfg config.Config, sessionID, defaultConfigPath, profilePath string) *Runner {
	return &Runner{
		cfg:               cfg,
		sessionID:         sessionID,
		logger:            log.New(os.Stdout, fmt.Sprintf("[session:%s] ", sessionID), log.LstdFlags),
		reader:            bufio.NewReader(os.Stdin),
		defaultConfigPath: defaultConfigPath,
		profilePath:       profilePath,
	}
}

func (r *Runner) Run() error {
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
		r.logger.Printf("Input received (stub): %s", line)
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
		r.logger.Printf("Context: max_recent=%d summarize_every=%d summarize_at=%d%%", r.cfg.Context.MaxRecentOutputs, r.cfg.Context.SummarizeEvery, r.cfg.Context.SummarizeAtPercent)
	case "ledger":
		return r.handleLedger(args)
	case "status":
		r.handleStatus()
	case "plan":
		return r.handlePlan(args)
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

func (r *Runner) handlePlan(args []string) error {
	r.setTask("plan")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: plan updates not permitted")
	}

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

func (r *Runner) handleRun(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /run <command> [args...]")
	}
	r.setTask(fmt.Sprintf("run %s", args[0]))
	defer r.clearTask()
	if !r.cfg.Tools.Shell.Enabled {
		return fmt.Errorf("shell execution disabled by config")
	}
	start := time.Now()
	requireApproval := r.cfg.Permissions.Level == "default" && r.cfg.Permissions.RequireApproval
	timeout := time.Duration(r.cfg.Tools.Shell.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runner := exec.Runner{
		Permissions:      exec.PermissionLevel(r.cfg.Permissions.Level),
		RequireApproval:  requireApproval,
		LogDir:           filepath.Join(r.cfg.Session.LogDir, r.sessionID, "logs"),
		Timeout:          timeout,
		Reader:           r.reader,
		ScopeNetworks:    r.cfg.Scope.Networks,
		ScopeTargets:     r.cfg.Scope.Targets,
		ScopeDenyTargets: r.cfg.Scope.DenyTargets,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	escCh, stopEsc, escErr := startEscWatcher()
	if escErr == nil && escCh != nil {
		r.logger.Printf("Press ESC to interrupt")
	} else if escErr != nil && r.isTTY() {
		r.logger.Printf("ESC interrupt unavailable: %v", escErr)
	}
	if escCh != nil {
		go func() {
			<-escCh
			cancel()
		}()
	}

	result, err := runner.RunCommandWithContext(ctx, args[0], args[1:]...)
	wasCanceled := errors.Is(err, context.Canceled)
	if stopEsc != nil {
		stopEsc()
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

	escCh, stopEsc, escErr := startEscWatcher()
	if escErr == nil && escCh != nil {
		r.logger.Printf("Press ESC to interrupt")
	} else if escErr != nil && r.isTTY() {
		r.logger.Printf("ESC interrupt unavailable: %v", escErr)
	}
	if escCh != nil {
		go func() {
			<-escCh
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
	}
	cmdArgs := []string{"-q", "-x", command}
	result, err := execRunner.RunCommandWithContext(ctx, "msfconsole", cmdArgs...)
	wasCanceled := errors.Is(err, context.Canceled)
	if stopEsc != nil {
		stopEsc()
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
	r.logger.Printf("Commands: /init /permissions /context /ledger /status /plan /summarize /run /msf /report /resume /stop /exit")
	r.logger.Printf("Example: /permissions readonly")
	r.logger.Printf("Session logs live under: %s", filepath.Clean(r.cfg.Session.LogDir))
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
	if r.cfg.Network.AssumeOffline {
		return memory.FallbackSummarizer{}
	}
	client := llm.NewLMStudioClient(r.cfg)
	return memory.ChainedSummarizer{
		Primary:  memory.LLMSummarizer{Client: client, Model: r.cfg.LLM.Model},
		Fallback: memory.FallbackSummarizer{},
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

func (r *Runner) readLine(prompt string) (string, error) {
	if prompt != "" {
		fmt.Print(prompt)
	}
	line, err := r.reader.ReadString('\n')
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
