package main

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestNormalizeArchiveWorkflowTaskDependenciesAddsCrackDependenciesToExtraction(t *testing.T) {
	t.Parallel()

	tasks := []orchestrator.TaskSpec{
		{
			TaskID:   "T-003",
			Title:    "Run Baseline Wordlist Crack",
			Goal:     "Run baseline wordlist cracking strategy.",
			Strategy: "baseline_crack",
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "john",
				Args:    []string{"--format=zip", "zip.hash"},
			},
		},
		{
			TaskID:   "T-004",
			Title:    "Run Wordlist + Rules Crack",
			Goal:     "Run wordlist + rules cracking strategy.",
			Strategy: "rules_crack",
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "john",
				Args:    []string{"--rules", "--format=zip", "zip.hash"},
			},
		},
		{
			TaskID:   "T-005",
			Title:    "Run Fallback Tooling (fcrackzip)",
			Goal:     "Run fallback tooling (fcrackzip) with wordlist.",
			Strategy: "fallback_crack",
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "fcrackzip",
				Args:    []string{"-u", "-D", "-p", "rockyou.txt", "secret.zip"},
			},
		},
		{
			TaskID:    "T-006",
			Title:     "Extract Archive Contents",
			Goal:      "Extract contents using recovered password.",
			Strategy:  "archive_extract",
			DependsOn: []string{"T-003"},
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "bash",
				Args:    []string{"-lc", "unzip -P $(cat john_output.txt) secret.zip"},
			},
		},
	}

	normalized, note := normalizeArchiveWorkflowTaskDependencies("Recover the password for secret.zip and extract contents", tasks)
	if strings.TrimSpace(note) == "" {
		t.Fatalf("expected normalization note")
	}
	if len(normalized) != len(tasks) {
		t.Fatalf("expected %d tasks, got %d", len(tasks), len(normalized))
	}
	var extract orchestrator.TaskSpec
	for _, task := range normalized {
		if task.TaskID == "T-006" {
			extract = task
			break
		}
	}
	for _, dep := range []string{"T-003", "T-004", "T-005"} {
		if !containsString(extract.DependsOn, dep) {
			t.Fatalf("expected extraction task to depend on %s, got %v", dep, extract.DependsOn)
		}
	}
}

func TestNormalizeArchiveWorkflowTaskDependenciesLeavesNonArchivePlansUnchanged(t *testing.T) {
	t.Parallel()

	tasks := []orchestrator.TaskSpec{
		{TaskID: "T-001", Title: "Discover Hosts", Goal: "Enumerate live hosts", DependsOn: []string{}},
		{TaskID: "T-002", Title: "Service Scan", Goal: "Run nmap service scan", DependsOn: []string{"T-001"}},
	}
	normalized, note := normalizeArchiveWorkflowTaskDependencies("Discover hosts on local network", tasks)
	if strings.TrimSpace(note) != "" {
		t.Fatalf("expected no note for non-archive workflow, got %q", note)
	}
	if len(normalized) != len(tasks) {
		t.Fatalf("expected %d tasks, got %d", len(tasks), len(normalized))
	}
	if !containsString(normalized[1].DependsOn, "T-001") || len(normalized[1].DependsOn) != 1 {
		t.Fatalf("expected non-archive deps unchanged, got %v", normalized[1].DependsOn)
	}
}

