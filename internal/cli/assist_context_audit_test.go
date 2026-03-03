package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestWriteAssistContextAuditAppendsRecord(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-audit", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	input := assist.Input{
		Scope:       []string{"internal"},
		Targets:     []string{"127.0.0.1"},
		Summary:     "summary text",
		KnownFacts:  []string{"fact one", "fact two"},
		Focus:       "focus text",
		Plan:        "1) do x",
		Inventory:   "inventory text",
		ChatHistory: "User: hello",
		RecentLog:   "recent observation",
		Playbooks:   "playbook hint",
		Tools:       "tool hint",
		WorkingDir:  "/tmp",
	}
	suggestion := assist.Suggestion{
		Type:     "command",
		Decision: "retry_modified",
		Command:  "list_dir",
		Args:     []string{"."},
		Summary:  "Inspect workspace",
	}
	meta := assist.LLMSuggestMetadata{
		Model:           "qwen",
		ParseRepairUsed: true,
		PrimaryResponse: "{\"type\":\"command\"}",
	}

	if err := r.writeAssistContextAudit(sessionDir, "test goal", "execute-step", input, suggestion, nil, true, meta); err != nil {
		t.Fatalf("writeAssistContextAudit: %v", err)
	}

	auditPath := filepath.Join(sessionDir, "artifacts", "assist", "context_audit.jsonl")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}

	lines := splitNonEmptyLines(string(data))
	if len(lines) != 1 {
		t.Fatalf("expected 1 audit line, got %d", len(lines))
	}

	var rec assistContextAuditRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if rec.Mode != "execute-step" {
		t.Fatalf("mode mismatch: %q", rec.Mode)
	}
	if rec.Suggestion.Command != "list_dir" {
		t.Fatalf("suggestion command mismatch: %q", rec.Suggestion.Command)
	}
	if !rec.LLM.Attempted || rec.LLM.Model != "qwen" {
		t.Fatalf("llm metadata mismatch: %+v", rec.LLM)
	}
	if rec.InputSizes.KnownFactsCount != 2 {
		t.Fatalf("known facts count mismatch: %d", rec.InputSizes.KnownFactsCount)
	}
}

func splitNonEmptyLines(text string) []string {
	out := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
