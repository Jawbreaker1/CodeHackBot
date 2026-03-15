package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestObservationExcerptIncludesHeadAndTailWhenTruncated(t *testing.T) {
	lines := []string{
		"l1", "l2", "l3", "l4", "l5", "l6",
		"l7", "l8", "l9", "l10", "l11", "l12", "l13", "l14",
	}
	text := strings.Join(lines, "\n")
	got := observationExcerpt(text, 8)

	if !strings.Contains(got, "l1 / l2 / l3 / l4") {
		t.Fatalf("expected head lines in excerpt, got: %s", got)
	}
	if !strings.Contains(got, "l11 / l12 / l13 / l14") {
		t.Fatalf("expected tail lines in excerpt, got: %s", got)
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("expected truncation marker, got: %s", got)
	}
}

func TestObservationExcerptReturnsAllWhenShort(t *testing.T) {
	text := "a\nb\nc"
	got := observationExcerpt(text, 8)
	if got != "a / b / c" {
		t.Fatalf("unexpected short excerpt: %q", got)
	}
}

func TestUpdateTaskFoundationKeepsLongGoalContext(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-focus-long", "", "")

	goal := "Scan internal lab network and identify the requested endpoint with high confidence, then verify only that endpoint using non-intrusive checks. " +
		strings.Repeat("detail ", 80) +
		"TAIL_MARKER_DO_NOT_DROP"
	r.updateTaskFoundation(goal)

	focusPath := filepath.Join(cfg.Session.LogDir, r.sessionID, "focus.md")
	data, err := os.ReadFile(focusPath)
	if err != nil {
		t.Fatalf("read focus: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "TAIL_MARKER_DO_NOT_DROP") {
		t.Fatalf("expected focus to retain long-goal tail marker, got: %q", content)
	}
}
