package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMergesConfigFiles(t *testing.T) {
	temp := t.TempDir()
	defaultPath := filepath.Join(temp, "default.json")
	profilePath := filepath.Join(temp, "profile.json")
	sessionPath := filepath.Join(temp, "session.json")

	writeJSON(t, defaultPath, map[string]any{
		"permissions": map[string]any{
			"level":            "default",
			"require_approval": true,
		},
		"context": map[string]any{
			"max_recent_outputs":    20,
			"summarize_every_steps": 8,
			"summarize_at_percent":  70,
		},
		"session": map[string]any{
			"log_dir": "sessions",
		},
	})

	writeJSON(t, profilePath, map[string]any{
		"permissions": map[string]any{
			"level":            "all",
			"require_approval": false,
		},
		"context": map[string]any{
			"max_recent_outputs": 5,
		},
	})

	writeJSON(t, sessionPath, map[string]any{
		"context": map[string]any{
			"summarize_every_steps": 2,
		},
	})

	cfg, _, err := Load(defaultPath, profilePath, sessionPath)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.Permissions.Level != "all" {
		t.Fatalf("permissions level mismatch: got %s", cfg.Permissions.Level)
	}
	if cfg.Permissions.RequireApproval != false {
		t.Fatalf("require_approval mismatch: got %t", cfg.Permissions.RequireApproval)
	}
	if cfg.Context.MaxRecentOutputs != 5 {
		t.Fatalf("max_recent_outputs mismatch: got %d", cfg.Context.MaxRecentOutputs)
	}
	if cfg.Context.SummarizeEvery != 2 {
		t.Fatalf("summarize_every mismatch: got %d", cfg.Context.SummarizeEvery)
	}
	if cfg.Context.SummarizeAtPercent != 70 {
		t.Fatalf("summarize_at_percent mismatch: got %d", cfg.Context.SummarizeAtPercent)
	}
	if cfg.Session.LogDir != "sessions" {
		t.Fatalf("log_dir mismatch: got %s", cfg.Session.LogDir)
	}
}

func TestSaveWritesJSON(t *testing.T) {
	temp := t.TempDir()
	path := filepath.Join(temp, "out.json")
	var cfg Config
	cfg.Agent.Name = "BirdHackBot"
	cfg.Context.MaxRecentOutputs = 15
	cfg.Permissions.Level = "default"
	cfg.Permissions.RequireApproval = true

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Read saved file: %v", err)
	}
	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal saved file: %v", err)
	}
	if decoded.Agent.Name != "BirdHackBot" {
		t.Fatalf("agent name mismatch: got %s", decoded.Agent.Name)
	}
	if decoded.Context.MaxRecentOutputs != 15 {
		t.Fatalf("max_recent_outputs mismatch: got %d", decoded.Context.MaxRecentOutputs)
	}
}

func writeJSON(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
