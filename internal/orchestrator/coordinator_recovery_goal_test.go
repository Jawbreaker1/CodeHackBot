package orchestrator

import (
	"strings"
	"testing"
)

func TestBuildAdaptiveRecoveryGoalIncludesCommandArgsAndLog(t *testing.T) {
	t.Parallel()

	goal := buildAdaptiveRecoveryGoal("execution_failure", "T-03", map[string]any{
		"reason":   "command_failed",
		"error":    "exit status 2",
		"command":  "awk",
		"args":     []any{"/Nmap scan report for/ {host=$NF} /PORT/ {print host, $0}", "/tmp/service_scan_output.txt"},
		"log_path": "/tmp/orch/task-T-03.log",
	})

	for _, want := range []string{
		"Recover from execution_failure on task T-03",
		"Failed command: awk /Nmap scan report for/ {host=$NF} /PORT/ {print host, $0} /tmp/service_scan_output.txt.",
		"Failure reason: command_failed.",
		"Error: exit status 2.",
		"Failure log: /tmp/orch/task-T-03.log.",
		"Worker mode is non-interactive: do not ask the operator questions;",
	} {
		if !strings.Contains(goal, want) {
			t.Fatalf("goal missing %q\nfull: %s", want, goal)
		}
	}
}
