package cli

import (
	"bufio"
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
		line, err := r.readLine("BirdHackBot> ")
		if err != nil && err != io.EOF {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if err == io.EOF {
				_ = r.handleStop()
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
	case "run":
		return r.handleRun(args)
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

func (r *Runner) handleRun(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /run <command> [args...]")
	}
	if !r.cfg.Tools.Shell.Enabled {
		return fmt.Errorf("shell execution disabled by config")
	}
	requireApproval := r.cfg.Permissions.Level == "default" && r.cfg.Permissions.RequireApproval
	timeout := time.Duration(r.cfg.Tools.Shell.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runner := exec.Runner{
		Permissions:     exec.PermissionLevel(r.cfg.Permissions.Level),
		RequireApproval: requireApproval,
		LogDir:          filepath.Join(r.cfg.Session.LogDir, r.sessionID, "logs"),
		Timeout:         timeout,
		Reader:          r.reader,
	}
	result, err := runner.RunCommand(args[0], args[1:]...)
	if result.LogPath != "" {
		r.logger.Printf("Log saved: %s", result.LogPath)
	}
	if r.cfg.Session.LedgerEnabled && result.LogPath != "" {
		sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
		if ledgerErr := session.AppendLedger(sessionDir, r.cfg.Session.LedgerFilename, strings.Join(append([]string{args[0]}, args[1:]...), " "), result.LogPath, ""); ledgerErr != nil {
			r.logger.Printf("Ledger update failed: %v", ledgerErr)
		}
	}
	if err != nil {
		return err
	}
	if result.Output != "" {
		r.logger.Printf("Output:\\n%s", result.Output)
	}
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

func (r *Runner) Stop() {
	r.stopOnce.Do(func() {
		if err := r.handleStop(); err != nil {
			r.logger.Printf("Session stop error: %v", err)
		}
	})
}

func (r *Runner) printHelp() {
	r.logger.Printf("Commands: /init /permissions /context /ledger /run /resume /stop /exit")
	r.logger.Printf("Example: /permissions readonly")
	r.logger.Printf("Session logs live under: %s", filepath.Clean(r.cfg.Session.LogDir))
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
