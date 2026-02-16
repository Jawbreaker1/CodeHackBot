package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWorkingIndicatorMessageNoOutput(t *testing.T) {
	started := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)
	now := started.Add(7 * time.Second)
	msg := workingIndicatorMessage("run bash", started, now, false, time.Time{})
	if !strings.Contains(msg, "no output yet") {
		t.Fatalf("expected no-output hint, got %q", msg)
	}
	if !strings.Contains(msg, "run bash") {
		t.Fatalf("expected task in message, got %q", msg)
	}
}

func TestWorkingIndicatorMessageStalledOutput(t *testing.T) {
	started := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)
	now := started.Add(14 * time.Second)
	lastOutput := started.Add(6 * time.Second)
	msg := workingIndicatorMessage("tool", started, now, true, lastOutput)
	if !strings.Contains(msg, "waiting for output") {
		t.Fatalf("expected waiting-for-output hint, got %q", msg)
	}
}

func TestWorkingIndicatorMessageSuppressesWhenOutputIsRecent(t *testing.T) {
	started := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)
	now := started.Add(9 * time.Second)
	lastOutput := started.Add(8 * time.Second)
	msg := workingIndicatorMessage("tool", started, now, true, lastOutput)
	if msg != "" {
		t.Fatalf("expected empty message for recent output, got %q", msg)
	}
}

func TestActivityWriterStatusLineDoesNotMarkOutputSeen(t *testing.T) {
	var buf bytes.Buffer
	writer := newActivityWriter(&buf)
	writer.writeStatusLine("... still working on tool (00:10 elapsed, no output yet)")

	if writer.HasOutput() {
		t.Fatalf("status line must not mark command output as seen")
	}
	got := buf.String()
	if !strings.HasPrefix(got, "\n... still working") {
		t.Fatalf("unexpected status line prefix: %q", got)
	}
	if !strings.HasSuffix(got, ")\n") {
		t.Fatalf("expected trailing newline, got %q", got)
	}
}

func TestFormatWorkingCommandTaskIncludesArgs(t *testing.T) {
	got := formatWorkingCommandTask("tool", "curl", []string{"-I", "https://example.com"})
	want := "tool curl -I https://example.com"
	if got != want {
		t.Fatalf("unexpected task label:\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatWorkingCommandTaskTruncates(t *testing.T) {
	args := []string{strings.Repeat("a", 300)}
	got := formatWorkingCommandTask("tool", "bash", args)
	if len([]rune(got)) > workingIndicatorTaskMaxRunes {
		t.Fatalf("task label too long: %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis for truncated task label, got %q", got)
	}
}
