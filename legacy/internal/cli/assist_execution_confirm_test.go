package cli

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func TestAssistActionPreviewCommand(t *testing.T) {
	s := assist.Suggestion{
		Type:    "command",
		Command: "nmap",
		Args:    []string{"-sV", "127.0.0.1"},
	}
	got := assistActionPreview(s)
	if !strings.Contains(got, "nmap -sV 127.0.0.1") {
		t.Fatalf("unexpected preview: %q", got)
	}
}

func TestAssistActionPreviewTool(t *testing.T) {
	s := assist.Suggestion{
		Type: "tool",
		Tool: &assist.ToolSpec{
			Name: "zip_helper",
			Run:  assist.ToolRun{Command: "bash", Args: []string{"run.sh"}},
		},
	}
	got := assistActionPreview(s)
	if !strings.Contains(got, "tool: bash run.sh") {
		t.Fatalf("unexpected preview: %q", got)
	}
}

func TestStartAssistRuntimeResetsExecutionApprovalOnGoalChange(t *testing.T) {
	r := &Runner{
		assistExecApproved: true,
		assistExecGoal:     "goal a",
	}
	r.startAssistRuntime("goal b", "execute-step", newAssistBudget("goal b", 4))
	if r.assistExecApproved {
		t.Fatalf("expected approval reset on goal change")
	}
	if r.assistExecGoal != "goal b" {
		t.Fatalf("expected assistExecGoal updated, got %q", r.assistExecGoal)
	}
}
