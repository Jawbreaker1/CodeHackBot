package cli

import (
	"strings"
	"testing"
	"time"
)

func TestPreviewOutput(t *testing.T) {
	input := "a\nb\nc\nd\ne\nf\ng"
	preview := previewOutput(input, 5)
	lines := strings.Split(preview, "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines (5 + ...), got %d", len(lines))
	}
	if lines[5] != "..." {
		t.Fatalf("expected ellipsis line, got %q", lines[5])
	}
}

func TestRenderExecSummaryIncludesCommand(t *testing.T) {
	summary := renderExecSummary("run echo", "echo", []string{"hi"}, 150*time.Millisecond, "log.txt", "appended", "hi", nil)
	if !strings.Contains(summary, "Command: echo hi") {
		t.Fatalf("missing command in summary")
	}
	if !strings.Contains(summary, "Ledger: appended") {
		t.Fatalf("missing ledger status")
	}
}
