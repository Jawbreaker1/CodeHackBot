package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestToolsSummaryReadsManifest(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-tools", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	toolsDir := filepath.Join(sessionDir, "artifacts", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `[
  {"time":"t1","name":"parser","language":"python","purpose":"parse nmap","run":"python parser.py","files":["x"],"hashes":{"x":"h"}}
]`
	if err := os.WriteFile(filepath.Join(toolsDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	summary := r.toolsSummary(sessionDir, 10)
	if !strings.Contains(summary, "parser (python): python parser.py") {
		t.Fatalf("unexpected summary:\n%s", summary)
	}
}
