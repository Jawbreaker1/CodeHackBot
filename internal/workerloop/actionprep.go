package workerloop

import (
	"fmt"
	"os/exec"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
)

func prepareAction(resp Response) (execx.Action, *ctxpacket.ExecutionResult) {
	command := strings.TrimSpace(resp.Command)
	if command == "" {
		return execx.Action{}, validationFailure("(none)", "action command is required", "invalid_action", "invalid_action")
	}

	if resp.UseShell {
		if _, err := exec.LookPath("/bin/sh"); err != nil {
			return execx.Action{}, validationFailure(command, "shell runtime is unavailable", "not_executable", "not_executable")
		}
		return execx.Action{Command: command, Cwd: ".", UseShell: true}, nil
	}

	parts := strings.Fields(command)
	if len(parts) == 0 {
		return execx.Action{}, validationFailure(command, "action command is required", "invalid_action", "invalid_action")
	}
	if _, err := exec.LookPath(parts[0]); err != nil {
		return execx.Action{}, validationFailure(command, fmt.Sprintf("command %q is not executable", parts[0]), "not_executable", "not_executable")
	}
	return execx.Action{
		Command:  parts[0],
		Args:     parts[1:],
		Cwd:      ".",
		UseShell: false,
	}, nil
}

func validationFailure(action, summary, signal, failureClass string) *ctxpacket.ExecutionResult {
	return &ctxpacket.ExecutionResult{
		Action:        strings.TrimSpace(action),
		ExitStatus:    "not_executed",
		OutputSummary: summary,
		Assessment:    "failed",
		Signals:       []string{signal},
		FailureClass:  failureClass,
	}
}
