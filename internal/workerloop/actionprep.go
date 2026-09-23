package workerloop

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
)

func prepareAction(resp Response, cwd string) (execx.Action, *ctxpacket.ExecutionResult) {
	command := strings.TrimSpace(resp.Command)
	if command == "" {
		return execx.Action{}, validationFailure("(none)", "bash command is required", "invalid_action", "invalid_action")
	}
	resolvedCwd, err := filepath.Abs(cwd)
	if err != nil {
		return execx.Action{}, validationFailure(command, "working directory could not be resolved", "invalid_action", "invalid_action")
	}
	cwd = resolvedCwd

	if resp.UseShell {
		if len(resp.Args) != 0 {
			return execx.Action{}, validationFailure(command, "shell mode must put the entire Bash script in command and omit args", "invalid_action", "invalid_action")
		}
		if _, err := exec.LookPath("/bin/bash"); err != nil {
			return execx.Action{}, validationFailure(command, "shell runtime is unavailable", "not_executable", "not_executable")
		}
		return execx.Action{Command: command, Cwd: cwd, UseShell: true}, nil
	}
	lookupCommand := command
	if !filepath.IsAbs(command) && strings.ContainsRune(command, '/') {
		lookupCommand = filepath.Join(cwd, command)
	}
	if _, err := exec.LookPath(lookupCommand); err != nil {
		return execx.Action{}, validationFailure(command, fmt.Sprintf("command %q is not executable; use command for the executable and args for literal arguments, or explicitly select use_shell", command), "not_executable", "not_executable")
	}
	return execx.Action{
		Command:  command,
		Args:     append([]string(nil), resp.Args...),
		Cwd:      cwd,
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
