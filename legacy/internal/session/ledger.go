package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func AppendLedger(sessionDir, filename, command, logPath, notes string) error {
	if filename == "" {
		filename = "ledger.md"
	}
	ledgerPath := filepath.Join(sessionDir, filename)
	if err := ensureLedgerHeader(ledgerPath); err != nil {
		return err
	}
	entry := fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
		"",
		escapePipes(command),
		escapePipes(logPath),
		escapePipes(time.Now().UTC().Format(time.RFC3339)),
		escapePipes(notes),
	)
	f, err := os.OpenFile(ledgerPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("append ledger: %w", err)
	}
	return nil
}

func ensureLedgerHeader(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	content := "# Evidence Ledger\n\n| Finding | Command | Log Path | Timestamp | Notes |\n| --- | --- | --- | --- | --- |\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write ledger header: %w", err)
	}
	return nil
}

func escapePipes(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}
