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

func TestNormalizeArchiveWorkflowTaskDependenciesPromotesUnderspecifiedExtractionToAssist(t *testing.T) {
	t.Parallel()

	tasks := []orchestrator.TaskSpec{
		{
			TaskID:   "T-001",
			Title:    "Extract Hash",
			Goal:     "Extract hash material from zip",
			Strategy: "hash_extraction",
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "zip2john",
				Args:    []string{"secret.zip"},
			},
		},
		{
			TaskID:   "T-002",
			Title:    "Crack Password",
			Goal:     "Run john wordlist crack",
			Strategy: "bounded_crack",
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "john",
				Args:    []string{"secret.zip.hash"},
			},
		},
		{
			TaskID:    "T-003",
			Title:     "Validate Password Recovery",
			Goal:      "Validate password recovery by extracting a harmless token",
			Strategy:  "validation",
			DependsOn: []string{"T-002"},
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "unzip",
				Args:    nil,
			},
		},
	}

	normalized, note := normalizeArchiveWorkflowTaskDependencies("Recover password for secret.zip and extract contents", tasks)
	if !strings.Contains(strings.ToLower(note), "promoted") {
		t.Fatalf("expected promotion note, got %q", note)
	}
	if len(normalized) != len(tasks) {
		t.Fatalf("expected %d tasks, got %d", len(tasks), len(normalized))
	}

	var validation orchestrator.TaskSpec
	for _, task := range normalized {
		if task.TaskID == "T-003" {
			validation = task
			break
		}
	}
	if !strings.EqualFold(strings.TrimSpace(validation.Action.Type), "assist") {
		t.Fatalf("expected T-003 action type assist, got %q", validation.Action.Type)
	}
	if strings.TrimSpace(validation.Action.Command) != "" {
		t.Fatalf("expected promoted assist action command to be cleared, got %q", validation.Action.Command)
	}
	if strings.TrimSpace(validation.Action.Prompt) == "" {
		t.Fatalf("expected promoted assist action to include prompt")
	}
}

func TestNormalizeArchiveWorkflowTaskDependenciesPromotesUnderspecifiedArchiveTaskWithoutCrackTask(t *testing.T) {
	t.Parallel()

	tasks := []orchestrator.TaskSpec{
		{
			TaskID:   "T-01",
			Title:    "Analyze archive",
			Goal:     "Extract archive metadata and attempt decryption using known weak strategies",
			Strategy: "crack_validation",
			Action: orchestrator.TaskAction{
				Type:    "command",
				Command: "unzip",
			},
		},
	}

	normalized, note := normalizeArchiveWorkflowTaskDependencies("Analyze local secret.zip", tasks)
	if !strings.Contains(strings.ToLower(note), "promoted") {
		t.Fatalf("expected promotion note, got %q", note)
	}
	if !strings.EqualFold(strings.TrimSpace(normalized[0].Action.Type), "assist") {
		t.Fatalf("expected assist promotion, got %q", normalized[0].Action.Type)
	}
}
