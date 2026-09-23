package workerloop

import (
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"strings"
	"testing"
)

func TestBuildRunningSummary(t *testing.T) {
	summary := buildRunningSummary(
		"inspect archive",
		ctxpacket.ExecutionResult{
			Action:        "file ./secret.zip",
			ExitStatus:    "0",
			Assessment:    "suspicious",
			Signals:       []string{"error_text", "incorrect_password"},
			OutputSummary: "stdout: ./secret.zip: ASCII text",
		},
		[]ctxpacket.ExecutionResult{
			{Action: "ls -la ./secret.zip", ExitStatus: "0"},
		},
	)
	for _, want := range []string{
		"Status: needs interpretation.",
		`Evidence: "file ./secret.zip" exited with 0.`,
		"Assessment: suspicious.",
		"Signals: error_text, incorrect_password.",
		"Key output: stdout: ./secret.zip: ASCII text.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q in %q", want, summary)
		}
	}
	for _, unwanted := range []string{
		"Objective:",
		"Recent prior results retained:",
		"Current status:",
	} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("summary unexpectedly contains %q in %q", unwanted, summary)
		}
	}
}

func TestBuildRunningSummaryDescribesLatestExecution(t *testing.T) {
	summary := buildRunningSummary(
		"extract archive",
		ctxpacket.ExecutionResult{
			Action:        "find /home/johan -name \"secret.zip\"",
			ExitStatus:    "0",
			OutputSummary: "stdout: /home/johan/.../secret.zip",
			Assessment:    "success",
		},
		[]ctxpacket.ExecutionResult{
			{
				Action:        "unzip -t secret.zip",
				ExitStatus:    "0",
				OutputSummary: "stdout: unable to get password",
				Assessment:    "suspicious",
				Signals:       []string{"incorrect_password"},
			},
		},
	)
	for _, want := range []string{
		"Status: in progress.",
		"Assessment: success.",
		"Key output: stdout: /home/johan/.../secret.zip.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q in %q", want, summary)
		}
	}
}

func TestBuildRunningSummaryCompactsOnlyVeryLongFields(t *testing.T) {
	longAction := "printf " + strings.Repeat("a", 700)
	longOutput := "stdout: " + strings.Repeat("b", 2200)
	summary := buildRunningSummary(
		"inspect archive",
		ctxpacket.ExecutionResult{
			Action:        longAction,
			ExitStatus:    "0",
			Assessment:    "success",
			OutputSummary: longOutput,
		},
		nil,
	)
	if len(summary) >= len(longAction)+len(longOutput) {
		t.Fatalf("summary was not compacted: len(summary)=%d", len(summary))
	}
	if !strings.Contains(summary, "...") {
		t.Fatalf("summary missing compacted marker: %q", summary)
	}
}

func TestBuildRunningSummaryKeepsModeratelyLongFields(t *testing.T) {
	longAction := "printf " + strings.Repeat("a", 260)
	longOutput := "stdout: " + strings.Repeat("b", 700)
	summary := buildRunningSummary(
		"inspect archive",
		ctxpacket.ExecutionResult{
			Action:        longAction,
			ExitStatus:    "0",
			Assessment:    "success",
			OutputSummary: longOutput,
		},
		nil,
	)
	if !strings.Contains(summary, longAction) {
		t.Fatalf("summary should preserve action without compaction: %q", summary)
	}
	if !strings.Contains(summary, longOutput) {
		t.Fatalf("summary should preserve evidence without compaction: %q", summary)
	}
}

func TestBuildRunningSummaryInterruptedExecution(t *testing.T) {
	summary := buildRunningSummary(
		"scan router",
		ctxpacket.ExecutionResult{
			Action:        "nmap -sV --top-ports 1000 192.168.50.1",
			ExitStatus:    "-1",
			Assessment:    "ambiguous",
			OutputSummary: "(none)",
			Signals:       []string{"execution_timeout"},
			FailureClass:  "execution_interrupted",
		},
		nil,
	)
	for _, want := range []string{
		"Status: in progress.",
		`Evidence: "nmap -sV --top-ports 1000 192.168.50.1" exited with -1.`,
		"Execution was interrupted before the active work completed.",
		"Assessment: ambiguous.",
		"Signals: execution_timeout.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q in %q", want, summary)
		}
	}
	if strings.Contains(summary, "encountered a failure") {
		t.Fatalf("summary incorrectly treated interrupted work as failure: %q", summary)
	}
}

func TestPrepareActionPreservesDirectArgumentsAndChecksExecutability(t *testing.T) {
	action, validationFailure := prepareAction(Response{Type: "bash", Command: "printf", Args: []string{"hello"}, UseShell: false}, t.TempDir())
	if validationFailure != nil {
		t.Fatalf("prepareAction() validation failure = %#v", validationFailure)
	}
	if action.Command != "printf" {
		t.Fatalf("action.Command = %q", action.Command)
	}
	if len(action.Args) != 1 || action.Args[0] != "hello" {
		t.Fatalf("action.Args = %#v", action.Args)
	}
}
