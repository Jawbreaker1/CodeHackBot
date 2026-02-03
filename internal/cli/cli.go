package cli

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
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
		r.logger.Printf("Init requested (stub)")
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
