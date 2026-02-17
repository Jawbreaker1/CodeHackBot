package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

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
		r.refreshLoggerPrefix()
		r.logger.Printf("Verbose logging enabled")
	case "off", "false", "0":
		r.cfg.UI.Verbose = false
		r.refreshLoggerPrefix()
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
	usage, usageErr := r.contextUsageSnapshot()
	active, label, started := r.llmStatus()
	llmConfigured := strings.TrimSpace(r.cfg.LLM.BaseURL) != ""
	llmState := "ready"
	if !llmConfigured {
		llmState = "not configured"
	} else if until := r.llmGuard.DisabledUntil(); !until.IsZero() && !r.llmGuard.Allow() {
		llmState = fmt.Sprintf("cooldown until %s", until.Format(time.RFC3339))
	}
	if r.currentTask == "" {
		if !active {
			r.logger.Printf("Status: idle")
			r.logger.Printf("LLM: %s", llmState)
			r.logger.Printf("LLM model: %s", statusValueOrFallback(strings.TrimSpace(r.cfg.LLM.Model), "(unset)"))
			if r.assistRuntime.Active {
				r.logger.Printf("Assist budget: step=%d remaining=%d cap=%d hard=%d ext=%d stalls=%d mode=%s", r.assistRuntime.Step, r.assistRuntime.Remaining, r.assistRuntime.CurrentCap, r.assistRuntime.HardCap, r.assistRuntime.Extensions, r.assistRuntime.Stalls, statusValueOrFallback(r.assistRuntime.CurrentMode, "(none)"))
				r.logger.Printf("Assist reason: %s", statusValueOrFallback(r.assistRuntime.LastReason, "(none)"))
			}
			r.logContextUsage(usage, usageErr)
			return
		}
		r.logger.Printf("Status: idle")
		r.logger.Printf("LLM: %s (%s)", label, formatElapsed(time.Since(started)))
		r.logger.Printf("LLM state: %s", llmState)
		r.logger.Printf("LLM model: %s", statusValueOrFallback(strings.TrimSpace(r.cfg.LLM.Model), "(unset)"))
		if r.assistRuntime.Active {
			r.logger.Printf("Assist budget: step=%d remaining=%d cap=%d hard=%d ext=%d stalls=%d mode=%s", r.assistRuntime.Step, r.assistRuntime.Remaining, r.assistRuntime.CurrentCap, r.assistRuntime.HardCap, r.assistRuntime.Extensions, r.assistRuntime.Stalls, statusValueOrFallback(r.assistRuntime.CurrentMode, "(none)"))
			r.logger.Printf("Assist reason: %s", statusValueOrFallback(r.assistRuntime.LastReason, "(none)"))
		}
		r.logContextUsage(usage, usageErr)
		return
	}
	elapsed := formatElapsed(time.Since(r.currentTaskStart))
	r.logger.Printf("Status: running %s (%s)", r.currentTask, elapsed)
	if active {
		r.logger.Printf("LLM: %s (%s)", label, formatElapsed(time.Since(started)))
	}
	r.logger.Printf("LLM state: %s", llmState)
	r.logger.Printf("LLM model: %s", statusValueOrFallback(strings.TrimSpace(r.cfg.LLM.Model), "(unset)"))
	if r.assistRuntime.Active {
		r.logger.Printf("Assist budget: step=%d remaining=%d cap=%d hard=%d ext=%d stalls=%d mode=%s", r.assistRuntime.Step, r.assistRuntime.Remaining, r.assistRuntime.CurrentCap, r.assistRuntime.HardCap, r.assistRuntime.Extensions, r.assistRuntime.Stalls, statusValueOrFallback(r.assistRuntime.CurrentMode, "(none)"))
		r.logger.Printf("Assist reason: %s", statusValueOrFallback(r.assistRuntime.LastReason, "(none)"))
	}
	r.logContextUsage(usage, usageErr)
}

func (r *Runner) logContextUsage(usage contextUsage, usageErr error) {
	if usageErr == nil {
		r.logger.Printf("Context usage: %s", usage.statusLine())
		return
	}
	if r.cfg.UI.Verbose {
		r.logger.Printf("Context usage unavailable: %v", usageErr)
	}
}

func statusValueOrFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
	if err := manager.Summarize(ctx, summarizer, reason); err != nil {
		stopIndicator()
		return err
	}
	stopIndicator()
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
	usage := buildContextUsage(r.cfg, state, countNonEmptyFileLines(artifacts.ChatPath))

	r.logger.Printf("Context Summary:\n%s", fallbackBlock(summary))
	r.logger.Printf("Known Facts:\n%s", fallbackBlock(facts))
	r.logger.Printf("Focus:\n%s", fallbackBlock(focus))
	r.logger.Printf("Context State: steps_since_summary=%d recent_logs=%d last_summary_at=%s", state.StepsSinceSummary, len(state.RecentLogs), state.LastSummaryAt)
	r.logger.Printf("Context Usage: %s", usage.statusLine())
	r.logger.Printf("Context Buckets: logs=%s observations=%s chat=%s", usage.Logs.label(), usage.Observed.label(), usage.Chat.label())
	r.logger.Printf("Context Auto-Summary: steps=%s recent_logs=%s (%s)", usage.StepWindow.label(), usage.LogWindow.label(), usage.summarizeRemainingLine())
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
	r.restoreTTYLayout()
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
