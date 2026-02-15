package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestToolForgeWritesFilesRunsAndUpdatesManifest(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Permissions.Level = "all"
	cfg.Tools.Shell.Enabled = true
	cfg.Tools.Shell.TimeoutSeconds = 5

	r := NewRunner(cfg, "session-tool", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	scriptRel := "demo/run.sh"
	scriptPath := filepath.Join(sessionDir, "artifacts", "tools", scriptRel)
	tool := assist.ToolSpec{
		Language: "bash",
		Name:     "demo",
		Purpose:  "print a marker",
		Files: []assist.ToolFile{
			{Path: scriptRel, Content: "#!/bin/sh\necho tool-ok\n"},
		},
		Run: assist.ToolRun{
			Command: "sh",
			Args:    []string{scriptPath},
		},
	}

	if err := r.executeToolSuggestion(tool, false); err != nil {
		t.Fatalf("executeToolSuggestion: %v", err)
	}

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("script missing: %v", err)
	}
	if !strings.Contains(string(data), "tool-ok") {
		t.Fatalf("unexpected script content: %s", string(data))
	}

	manifestPath := filepath.Join(sessionDir, "artifacts", "tools", "manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if !strings.Contains(string(manifest), "\"name\": \"demo\"") {
		t.Fatalf("manifest missing tool entry: %s", string(manifest))
	}

	// Confirm the run produced a cmd log with output.
	logDir := filepath.Join(sessionDir, "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	found := false
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "cmd-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(logDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(content), "tool-ok") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cmd log to contain tool output")
	}
}

func TestToolForgeRejectsOutOfBoundsPaths(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Permissions.Level = "all"
	cfg.Tools.Shell.Enabled = true
	r := NewRunner(cfg, "session-tool-bad", "", "")
	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	tool := assist.ToolSpec{
		Language: "bash",
		Name:     "bad",
		Files:    []assist.ToolFile{{Path: "../escape.sh", Content: "echo nope\n"}},
		Run:      assist.ToolRun{Command: "sh", Args: []string{"-c", "echo ok"}},
	}
	if err := r.executeToolSuggestion(tool, false); err == nil {
		t.Fatalf("expected out-of-bounds error")
	}
}
