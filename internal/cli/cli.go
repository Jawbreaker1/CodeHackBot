package cli

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

type Runner struct {
	cfg       config.Config
	sessionID string
	logger    *log.Logger
}

func NewRunner(cfg config.Config, sessionID string) *Runner {
	return &Runner{
		cfg:       cfg,
		sessionID: sessionID,
		logger:    log.New(os.Stdout, fmt.Sprintf("[session:%s] ", sessionID), log.LstdFlags),
	}
}

func (r *Runner) Run() error {
	r.logger.Printf("BirdHackBot interactive mode. Type /help for commands.")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("BirdHackBot> ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if err := r.handleCommand(line); err != nil {
				r.logger.Printf("Command error: %v", err)
			}
			continue
		}
		r.logger.Printf("Input received (stub): %s", line)
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
	case "resume":
		return r.handleResume()
	case "exit", "quit":
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
		approved, err := confirm("Run inventory commands now?")
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

func (r *Runner) handleResume() error {
	root := r.cfg.Session.LogDir
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read sessions: %w", err)
	}
	r.logger.Printf("Available sessions:")
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		r.logger.Printf("- %s", entry.Name())
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter session id to resume (or blank to cancel): ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read session id: %w", err)
	}
	selection := strings.TrimSpace(line)
	if selection == "" {
		return nil
	}
	r.sessionID = selection
	r.logger.SetPrefix(fmt.Sprintf("[session:%s] ", r.sessionID))
	r.logger.Printf("Session switched to %s. Use --resume for full context load.", r.sessionID)
	return nil
}

func (r *Runner) printHelp() {
	r.logger.Printf("Commands: /init /permissions /context /ledger /resume /exit")
	r.logger.Printf("Example: /permissions readonly")
	r.logger.Printf("Session logs live under: %s", filepath.Clean(r.cfg.Session.LogDir))
}

func confirm(prompt string) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s [y/N]: ", prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" || answer == "n" || answer == "no" {
			return false, nil
		}
		if answer == "y" || answer == "yes" {
			return true, nil
		}
	}
}
