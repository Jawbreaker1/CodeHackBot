package main

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestNormalizePlannerCommandExecutabilityPromotesBareBoundCommand(t *testing.T) {
	t.Parallel()

	tasks := []orchestrator.TaskSpec{
		{
			TaskID:            "T-01",
			Title:             "Find archive",
			Goal:              "Locate secret.zip in the working directory",
			ExpectedArtifacts: []string{"secret.zip"},
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "ls",
			},
		},
		{
			TaskID:            "T-02",
			Title:             "Crack password",
			Goal:              "Run john against the extracted hash for secret.zip",
			DependsOn:         []string{"T-01"},
			DoneWhen:          []string{"password recovered"},
			FailWhen:          []string{"cracking failed"},
			ExpectedArtifacts: []string{"john.out"},
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "john",
			},
		},
	}

	normalized, note := normalizePlannerCommandExecutability(tasks)
	if !strings.Contains(strings.ToLower(note), "promoted") {
		t.Fatalf("expected promotion note, got %q", note)
	}
	if normalized[0].Action.Type != "assist" {
		t.Fatalf("expected T-01 promoted to assist, got %q", normalized[0].Action.Type)
	}
	if normalized[1].Action.Type != "assist" {
		t.Fatalf("expected T-02 promoted to assist, got %q", normalized[1].Action.Type)
	}
	if !strings.Contains(normalized[1].Action.Prompt, "secret.zip") {
		t.Fatalf("expected assist prompt to retain task anchors, got %q", normalized[1].Action.Prompt)
	}
}

func TestNormalizePlannerCommandExecutabilityPreservesConcreteBoundCommand(t *testing.T) {
	t.Parallel()

	tasks := []orchestrator.TaskSpec{
		{
			TaskID:            "T-01",
			Title:             "Hash extract",
			Goal:              "Extract crackable hash material from secret.zip",
			ExpectedArtifacts: []string{"zip.hash"},
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "bash",
				Args:    []string{"-lc", "zip2john secret.zip > zip.hash"},
			},
		},
	}

	normalized, note := normalizePlannerCommandExecutability(tasks)
	if strings.TrimSpace(note) != "" {
		t.Fatalf("expected no normalization note, got %q", note)
	}
	if normalized[0].Action.Type != "command" {
		t.Fatalf("expected command preserved, got %q", normalized[0].Action.Type)
	}
}

func TestNormalizePlannerCommandExecutabilityPreservesAnchorlessBenignCommand(t *testing.T) {
	t.Parallel()

	tasks := []orchestrator.TaskSpec{
		{
			TaskID:            "T-01",
			Title:             "Check user",
			Goal:              "Determine current local user",
			DoneWhen:          []string{"user identified"},
			FailWhen:          []string{"whoami unavailable"},
			ExpectedArtifacts: []string{"whoami.log"},
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "whoami",
			},
		},
	}

	normalized, note := normalizePlannerCommandExecutability(tasks)
	if strings.TrimSpace(note) != "" {
		t.Fatalf("expected no normalization note, got %q", note)
	}
	if normalized[0].Action.Type != "command" {
		t.Fatalf("expected command preserved, got %q", normalized[0].Action.Type)
	}
}
