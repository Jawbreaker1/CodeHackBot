package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendLedgerCreatesAndAppends(t *testing.T) {
	temp := t.TempDir()
	ledgerPath := filepath.Join(temp, "ledger.md")

	if err := AppendLedger(temp, "ledger.md", "echo hi", "logs/cmd.log", "note"); err != nil {
		t.Fatalf("AppendLedger error: %v", err)
	}

	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Evidence Ledger") {
		t.Fatalf("missing header")
	}
	if !strings.Contains(content, "echo hi") {
		t.Fatalf("missing command entry")
	}
	if !strings.Contains(content, "logs/cmd.log") {
		t.Fatalf("missing log path entry")
	}
}
