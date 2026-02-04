package session

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

type ScaffoldOptions struct {
	RootDir           string
	SessionID         string
	PlanFilename      string
	InventoryFilename string
	LedgerFilename    string
	CreateLedger      bool
}

func CreateScaffold(opts ScaffoldOptions) (string, error) {
	if opts.RootDir == "" {
		opts.RootDir = "sessions"
	}
	if opts.PlanFilename == "" {
		opts.PlanFilename = "plan.md"
	}
	if opts.InventoryFilename == "" {
		opts.InventoryFilename = "inventory.md"
	}
	if opts.LedgerFilename == "" {
		opts.LedgerFilename = "ledger.md"
	}

	sessionDir := filepath.Join(opts.RootDir, opts.SessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}
	for _, name := range []string{"logs", "artifacts"} {
		if err := os.MkdirAll(filepath.Join(sessionDir, name), 0o755); err != nil {
			return "", fmt.Errorf("create %s dir: %w", name, err)
		}
	}

	if err := InitMeta(sessionDir, opts.SessionID); err != nil {
		return "", fmt.Errorf("write session meta: %w", err)
	}

	planPath := filepath.Join(sessionDir, opts.PlanFilename)
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		content := DefaultPlanContent()
		if err := os.WriteFile(planPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write plan: %w", err)
		}
	}

	inventoryPath := filepath.Join(sessionDir, opts.InventoryFilename)
	if _, err := os.Stat(inventoryPath); os.IsNotExist(err) {
		content := "# Inventory\n\nPending collection.\n"
		if err := os.WriteFile(inventoryPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write inventory: %w", err)
		}
	}

	if opts.CreateLedger {
		ledgerPath := filepath.Join(sessionDir, opts.LedgerFilename)
		if _, err := os.Stat(ledgerPath); os.IsNotExist(err) {
			content := "# Evidence Ledger\n\n| Finding | Command | Log Path | Timestamp | Notes |\n| --- | --- | --- | --- | --- |\n"
			if err := os.WriteFile(ledgerPath, []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("write ledger: %w", err)
			}
		}
	}

	if _, err := memory.EnsureArtifacts(sessionDir); err != nil {
		return "", fmt.Errorf("write context artifacts: %w", err)
	}

	return sessionDir, nil
}
