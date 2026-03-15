package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateScaffold(t *testing.T) {
	temp := t.TempDir()
	id := "test-session"

	sessionDir, err := CreateScaffold(ScaffoldOptions{
		RootDir:           temp,
		SessionID:         id,
		PlanFilename:      "plan.md",
		InventoryFilename: "inventory.md",
		LedgerFilename:    "ledger.md",
		CreateLedger:      true,
	})
	if err != nil {
		t.Fatalf("CreateScaffold error: %v", err)
	}

	expectedDir := filepath.Join(temp, id)
	if sessionDir != expectedDir {
		t.Fatalf("session dir mismatch: got %s want %s", sessionDir, expectedDir)
	}

	paths := []string{
		filepath.Join(sessionDir, "plan.md"),
		filepath.Join(sessionDir, "inventory.md"),
		filepath.Join(sessionDir, "ledger.md"),
		filepath.Join(sessionDir, "logs"),
		filepath.Join(sessionDir, "artifacts"),
		filepath.Join(sessionDir, "session.json"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected path missing: %s: %v", path, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(sessionDir, "session.json"))
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse session.json: %v", err)
	}
	if meta.ID != id {
		t.Fatalf("meta id mismatch: got %s want %s", meta.ID, id)
	}
	if meta.Status != "active" {
		t.Fatalf("meta status mismatch: got %s", meta.Status)
	}
}
