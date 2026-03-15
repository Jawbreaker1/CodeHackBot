package behavior

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBehaviorFrame(t *testing.T) {
	root := t.TempDir()
	agentsPath := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Agent Directives\n- Stay in scope."), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	frame, err := Load(root, "worker", map[string]string{
		"approval_mode": "default",
		"surface":       "interactive_worker",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if frame.RuntimeMode != "worker" {
		t.Fatalf("RuntimeMode = %q, want worker", frame.RuntimeMode)
	}
	if frame.AgentsPath != agentsPath {
		t.Fatalf("AgentsPath = %q, want %q", frame.AgentsPath, agentsPath)
	}
	if !strings.Contains(frame.AgentsText, "Stay in scope") {
		t.Fatalf("AgentsText missing AGENTS content: %q", frame.AgentsText)
	}
	if frame.Parameters["approval_mode"] != "default" {
		t.Fatalf("approval_mode = %q", frame.Parameters["approval_mode"])
	}
}

func TestLoadBehaviorFrameDefaultsRuntimeMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("rules"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	frame, err := Load(root, "", nil)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if frame.RuntimeMode != "worker" {
		t.Fatalf("RuntimeMode = %q, want worker", frame.RuntimeMode)
	}
}

func TestPromptTextIncludesStableSources(t *testing.T) {
	frame := Frame{
		SystemPrompt: "prompt",
		AgentsText:   "agents",
		RuntimeMode:  "orchestrator",
		Parameters: map[string]string{
			"alpha": "1",
			"beta":  "2",
		},
	}

	text := frame.PromptText()
	for _, want := range []string{
		"System prompt:",
		"prompt",
		"AGENTS.md:",
		"agents",
		"Runtime mode:",
		"orchestrator",
		"- alpha: 1",
		"- beta: 2",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("PromptText() missing %q in:\n%s", want, text)
		}
	}
}
