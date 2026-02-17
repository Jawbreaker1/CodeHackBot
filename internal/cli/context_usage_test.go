package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

func TestBuildContextUsage(t *testing.T) {
	cfg := config.Config{}
	cfg.Context.MaxRecentOutputs = 10
	cfg.Context.ChatHistoryLines = 20
	cfg.Context.SummarizeEvery = 4
	cfg.Context.SummarizeAtPercent = 80

	state := memory.State{
		StepsSinceSummary: 2,
		RecentLogs:        []string{"a", "b", "c", "d", "e", "f"},
		RecentObservations: []memory.Observation{
			{Command: "ls"},
			{Command: "whois"},
			{Command: "dig"},
			{Command: "curl"},
		},
	}

	usage := buildContextUsage(cfg, state, 10)
	if usage.BufferPercent != 60 {
		t.Fatalf("expected buffer percent 60, got %d", usage.BufferPercent)
	}
	if usage.SummarizePercent != 75 {
		t.Fatalf("expected summarize percent 75, got %d", usage.SummarizePercent)
	}
	if usage.OverallPercent != 75 {
		t.Fatalf("expected overall percent 75, got %d", usage.OverallPercent)
	}
	if usage.LogWindow.Cap != 8 {
		t.Fatalf("expected summarize log threshold 8, got %d", usage.LogWindow.Cap)
	}
}

func TestContextUsageStatusLineDisabled(t *testing.T) {
	cfg := config.Config{}
	usage := buildContextUsage(cfg, memory.State{}, 0)
	if usage.statusLine() != "disabled" {
		t.Fatalf("expected disabled status, got %q", usage.statusLine())
	}
}

func TestHandleCommandContextPrintsUsage(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Context.MaxRecentOutputs = 5
	cfg.Context.ChatHistoryLines = 5
	r := NewRunner(cfg, "session-context", "", "")

	var out bytes.Buffer
	r.logger.SetOutput(&out)
	if err := r.handleCommand("/context"); err != nil {
		t.Fatalf("handleCommand /context: %v", err)
	}
	logs := out.String()
	if !strings.Contains(logs, "Context usage:") {
		t.Fatalf("expected context usage output, got %q", logs)
	}
}
