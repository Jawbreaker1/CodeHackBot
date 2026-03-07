package orchestrator

import (
	"strings"
	"testing"
)

func TestBuildAdaptiveRecoveryGoalIncludesCommandArgsAndLog(t *testing.T) {
	t.Parallel()

	goal := buildAdaptiveRecoveryGoal("execution_failure", "T-03", map[string]any{
		"reason":                "command_failed",
		"error":                 "exit status 2",
		"command":               "awk",
		"args":                  []any{"/Nmap scan report for/ {host=$NF} /PORT/ {print host, $0}", "/tmp/service_scan_output.txt"},
		"log_path":              "/tmp/orch/task-T-03.log",
		"latest_result_summary": "command failed: awk /tmp/service_scan_output.txt | output: parse error",
		"latest_evidence_refs":  []any{"/tmp/service_scan_output.txt"},
		"latest_input_refs":     []any{"/tmp/service_scan_output.txt", "/tmp/scan.plan"},
	})

	for _, want := range []string{
		"Recover from execution_failure on task T-03",
		"Failed command: awk /Nmap scan report for/ {host=$NF} /PORT/ {print host, $0} /tmp/service_scan_output.txt.",
		"Failure reason: command_failed.",
		"Error: exit status 2.",
		"Latest command result: command failed: awk /tmp/service_scan_output.txt | output: parse error.",
		"Latest evidence refs: /tmp/service_scan_output.txt.",
		"Latest input refs: /tmp/service_scan_output.txt, /tmp/scan.plan.",
		"Failure log: /tmp/orch/task-T-03.log.",
		"Worker mode is non-interactive: do not ask the operator questions;",
	} {
		if !strings.Contains(goal, want) {
			t.Fatalf("goal missing %q\nfull: %s", want, goal)
		}
	}
}

func TestBuildAdaptiveRecoveryGoalSkipsNetworkHintForLocalFileFailure(t *testing.T) {
	t.Parallel()

	goal := buildAdaptiveRecoveryGoal("execution_failure", "T-06", map[string]any{
		"reason":  "command_failed",
		"error":   "exit status 82",
		"command": "unzip",
		"args":    []any{"-P", "badpass", "/home/johan/CodeHackBot/secret.zip"},
	})
	if strings.Contains(goal, "broad CIDR scan timed out") {
		t.Fatalf("did not expect network recovery hint for local file failure goal: %s", goal)
	}
}

func TestBuildAdaptiveRecoveryGoalIncludesNetworkHintForNetworkFailure(t *testing.T) {
	t.Parallel()

	goal := buildAdaptiveRecoveryGoal("execution_failure", "T-09", map[string]any{
		"reason":  "command_failed",
		"error":   "host timeout",
		"command": "nmap",
		"args":    []any{"-sV", "192.168.50.0/24"},
	})
	if !strings.Contains(goal, "broad CIDR scan timed out") {
		t.Fatalf("expected network recovery hint for network scan failure goal: %s", goal)
	}
}
