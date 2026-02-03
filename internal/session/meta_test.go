package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCloseMeta(t *testing.T) {
	temp := t.TempDir()
	id := "close-session"
	if err := os.MkdirAll(filepath.Join(temp, id), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sessionDir := filepath.Join(temp, id)

	if err := InitMeta(sessionDir, id); err != nil {
		t.Fatalf("InitMeta: %v", err)
	}
	if err := CloseMeta(sessionDir, id); err != nil {
		t.Fatalf("CloseMeta: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(sessionDir, "session.json"))
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse session.json: %v", err)
	}
	if meta.Status != "closed" {
		t.Fatalf("expected closed status, got %s", meta.Status)
	}
	if meta.EndedAt == "" {
		t.Fatalf("expected ended_at to be set")
	}
}
