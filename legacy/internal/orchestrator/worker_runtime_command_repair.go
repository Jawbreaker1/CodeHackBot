package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

const (
	commandContractRepairOutputBytes = 16 * 1024
	commandHelpSnapshotMaxBytes      = 8 * 1024
	commandHelpSnapshotTimeout       = 8 * time.Second
)

var requiredTargetOptionPattern = regexp.MustCompile(`(?is)no\s+([^()\n]{1,80})\s*\((--?[a-z0-9][a-z0-9_-]*)\)\s+specified`)

type commandContractRepairResult struct {
	Used    bool
	Command string
	Args    []string
	Output  []byte
	RunErr  error
	Note    string
}

func attemptCommandContractRepair(ctx context.Context, cfg WorkerRunConfig, task TaskSpec, scopePolicy *ScopePolicy, workDir, command string, args []string, output []byte, runErr error) (commandContractRepairResult, error) {
	if runErr == nil {
		return commandContractRepairResult{}, nil
	}
	if errorsIsTimeout(runErr) || errors.Is(runErr, errWorkerCommandInterrupted) {
		return commandContractRepairResult{}, nil
	}
	if !outputSuggestsCommandContractFailure(output) {
		return commandContractRepairResult{}, nil
	}
	if inferredFlag, inferredTarget, ok := inferRequiredTargetOptionFromOutput(task, scopePolicy, command, args, output); ok {
		heuristicArgs := prependRequiredOptionArg(args, inferredFlag, inferredTarget)
		if scopePolicy != nil && requiresCommandScopeValidation(command, heuristicArgs) {
			if err := scopePolicy.ValidateCommandTargets(command, heuristicArgs); err != nil {
				return commandContractRepairResult{}, fmt.Errorf("inferred command repair violated scope: %w", err)
			}
		}
		heuristicOutput, heuristicErr, execErr := executeCommandRepairAttempt(ctx, cfg, task, workDir, command, heuristicArgs)
		if execErr != nil {
			return commandContractRepairResult{}, execErr
		}
		note := fmt.Sprintf("command contract repair inferred required option %s from tool output", inferredFlag)
		if heuristicErr == nil {
			return commandContractRepairResult{
				Used:    true,
				Command: command,
				Args:    heuristicArgs,
				Output:  heuristicOutput,
				RunErr:  nil,
				Note:    note,
			}, nil
		}
		output = mergeCommandContractRepairOutput(output, heuristicOutput, note+" (attempt failed; escalating to assistant)")
		runErr = heuristicErr
		args = heuristicArgs
	}

	builder := cfg.resolveAssistantBuilder()
	model, mode, workerAssistant, err := builder()
	if err != nil {
		return commandContractRepairResult{}, fmt.Errorf("assistant unavailable: %w", err)
	}
	if workerAssistant == nil {
		return commandContractRepairResult{}, fmt.Errorf("assistant unavailable: nil assistant")
	}

	helpOutput, helpArg := collectCommandHelpSnapshot(ctx, command, workDir)
	repairInput := buildCommandContractRepairInput(cfg, task, command, args, runErr, output, helpOutput, helpArg, workDir)
	suggestCtx, cancel, _, _, suggestErr := newAssistCallContext(ctx)
	if suggestErr != nil {
		return commandContractRepairResult{}, fmt.Errorf("assist budget unavailable: %w", suggestErr)
	}
	defer cancel()
	suggestion, turnMeta, err := workerAssistant.Suggest(suggestCtx, repairInput)
	if err != nil {
		return commandContractRepairResult{}, fmt.Errorf("assist repair suggestion failed: %w", err)
	}
	if strings.TrimSpace(suggestion.Type) != "command" {
		return commandContractRepairResult{}, fmt.Errorf("assist repair returned non-command suggestion type=%q", strings.TrimSpace(suggestion.Type))
	}

	repairCommand := strings.TrimSpace(suggestion.Command)
	repairArgs := normalizeArgs(suggestion.Args)
	if repairCommand == "" {
		return commandContractRepairResult{}, fmt.Errorf("assist repair returned empty command")
	}
	if strings.EqualFold(repairCommand, command) && stringSlicesEqual(repairArgs, args) {
		return commandContractRepairResult{}, fmt.Errorf("assist repair suggested identical failing command")
	}
	if !cfg.Diagnostic {
		prepared := prepareRuntimeCommand(scopePolicy, task, repairCommand, repairArgs)
		repairCommand = prepared.Command
		repairArgs = prepared.Args
	}
	if scopePolicy != nil && requiresCommandScopeValidation(repairCommand, repairArgs) {
		if err := scopePolicy.ValidateCommandTargets(repairCommand, repairArgs); err != nil {
			return commandContractRepairResult{}, fmt.Errorf("assist repair violated scope: %w", err)
		}
	}

	repairedOutput, repairedErr, execErr := executeCommandRepairAttempt(ctx, cfg, task, workDir, repairCommand, repairArgs)
	if execErr != nil {
		return commandContractRepairResult{}, execErr
	}

	modeLabel := strings.TrimSpace(mode)
	if modeLabel == "" {
		modeLabel = strings.TrimSpace(turnMeta.AssistMode)
	}
	note := strings.TrimSpace(suggestion.Summary)
	if note == "" {
		note = fmt.Sprintf("command contract repair retry via assistant model=%s mode=%s", strings.TrimSpace(firstNonEmpty(turnMeta.Model, model)), modeLabel)
	} else {
		note = fmt.Sprintf("%s (assistant model=%s mode=%s)", note, strings.TrimSpace(firstNonEmpty(turnMeta.Model, model)), modeLabel)
	}
	return commandContractRepairResult{
		Used:    true,
		Command: repairCommand,
		Args:    repairArgs,
		Output:  repairedOutput,
		RunErr:  repairedErr,
		Note:    note,
	}, nil
}

func executeCommandRepairAttempt(ctx context.Context, cfg WorkerRunConfig, task TaskSpec, workDir, command string, args []string) ([]byte, error, error) {
	if isWorkerBuiltinCommand(command) {
		result := executeWorkerAssistCommand(ctx, cfg, task, command, args, workDir)
		return result.output, result.runErr, nil
	}
	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	commandEnv := os.Environ()
	if adaptedEnv, _, envErr := applyArchiveToolRuntimeEnv(commandEnv, task, command, workDir); envErr != nil {
		return nil, nil, fmt.Errorf("assist repair environment adaptation failed: %w", envErr)
	} else {
		commandEnv = adaptedEnv
	}
	cmd.Env = commandEnv
	output, runErr := runWorkerCommand(ctx, cmd, workerCommandStopGrace)
	return output, runErr, nil
}

func buildCommandContractRepairInput(cfg WorkerRunConfig, task TaskSpec, command string, args []string, runErr error, output []byte, helpOutput []byte, helpArg string, workDir string) assist.Input {
	commandLine := strings.TrimSpace(command + " " + strings.Join(args, " "))
	knownFacts := []string{
		fmt.Sprintf("Failed command: %s", strings.TrimSpace(commandLine)),
		fmt.Sprintf("Runtime error: %s", strings.TrimSpace(runErrString(runErr))),
	}
	if strings.TrimSpace(helpArg) != "" && len(helpOutput) > 0 {
		knownFacts = append(knownFacts, fmt.Sprintf("Tool help captured via %s", strings.TrimSpace(helpArg)))
	}
	failureOutput := truncateStringBytes(strings.TrimSpace(string(output)), commandContractRepairOutputBytes)
	helpText := truncateStringBytes(strings.TrimSpace(string(helpOutput)), commandHelpSnapshotMaxBytes)
	recentLog := strings.TrimSpace(strings.Join([]string{
		"failure_output:\n" + failureOutput,
		func() string {
			if helpText == "" {
				return ""
			}
			return "help_output:\n" + helpText
		}(),
	}, "\n\n"))
	focus := strings.TrimSpace(strings.Join([]string{
		"Task goal: " + strings.TrimSpace(task.Goal),
		"Expected artifacts: " + strings.Join(compactStrings(task.ExpectedArtifacts), ", "),
		"Done-when: " + strings.Join(compactStrings(task.DoneWhen), "; "),
	}, "\n"))
	return assist.Input{
		SessionID:  cfg.RunID,
		Scope:      append([]string{}, task.Targets...),
		Targets:    append([]string{}, task.Targets...),
		Summary:    "Command failed with usage/contract signal. Generate one corrected command.",
		KnownFacts: knownFacts,
		Focus:      focus,
		Goal:       "Repair the failed command invocation and return one corrected command for the same objective. Avoid repeating the same failing invocation.",
		WorkingDir: workDir,
		RecentLog:  recentLog,
		Mode:       "recover",
		Tools:      "Available tools: " + discoverAvailableFallbackTools(),
		Plan:       "Return type=command only. Prefer correcting the same tool invocation before switching tool families.",
	}
}

func collectCommandHelpSnapshot(parent context.Context, command string, workDir string) ([]byte, string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, ""
	}
	argsCandidates := [][]string{
		{"--help"},
		{"-h"},
	}
	for _, candidate := range argsCandidates {
		timeout := commandHelpSnapshotTimeout
		if remaining := remainingContextDuration(parent); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
		if timeout < 2*time.Second {
			continue
		}
		helpCtx, cancel := context.WithTimeout(parent, timeout)
		cmd := exec.Command(command, candidate...)
		cmd.Dir = workDir
		cmd.Env = os.Environ()
		output, _ := runWorkerCommand(helpCtx, cmd, workerCommandStopGrace)
		cancel()
		text := strings.TrimSpace(string(output))
		if text == "" {
			continue
		}
		return []byte(truncateStringBytes(text, commandHelpSnapshotMaxBytes)), strings.Join(candidate, " ")
	}
	return nil, ""
}

func outputSuggestsCommandContractFailure(output []byte) bool {
	lower := strings.ToLower(strings.TrimSpace(string(output)))
	if lower == "" {
		return false
	}
	needles := []string{
		"usage:",
		"invalid option",
		"unknown option",
		"unrecognized option",
		"requires a value",
		"requires an argument",
		"missing required",
		"missing argument",
		"no host (-host) specified",
		"no target specified",
		"try --help",
		"for help",
	}
	return containsAnySubstring(lower, needles...)
}

func inferRequiredTargetOptionFromOutput(task TaskSpec, scopePolicy *ScopePolicy, command string, args []string, output []byte) (string, string, bool) {
	text := strings.ToLower(strings.TrimSpace(string(output)))
	if text == "" {
		return "", "", false
	}
	matches := requiredTargetOptionPattern.FindAllStringSubmatch(text, 4)
	if len(matches) == 0 {
		return "", "", false
	}
	target := firstTaskTarget(task.Targets)
	if target == "" && scopePolicy != nil {
		target = strings.TrimSpace(scopePolicy.FirstAllowedTarget())
	}
	if target == "" {
		return "", "", false
	}
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		descriptor := strings.TrimSpace(match[1])
		flag := strings.TrimSpace(match[2])
		if flag == "" || strings.HasPrefix(flag, "---") {
			continue
		}
		if !looksLikeTargetDescriptor(descriptor) && !looksLikeTargetFlag(flag) {
			continue
		}
		if hasOptionFlag(args, flag) {
			continue
		}
		return flag, target, true
	}
	return "", "", false
}

func looksLikeTargetDescriptor(value string) bool {
	if value == "" {
		return false
	}
	needles := []string{"host", "target", "url", "address", "ip"}
	return containsAnySubstring(value, needles...)
}

func looksLikeTargetFlag(value string) bool {
	value = strings.ToLower(strings.TrimSpace(strings.TrimLeft(value, "-")))
	if value == "" {
		return false
	}
	needles := []string{"host", "target", "url", "address", "ip"}
	return containsAnySubstring(value, needles...)
}

func hasOptionFlag(args []string, flag string) bool {
	normalizedFlag := strings.ToLower(strings.TrimSpace(flag))
	if normalizedFlag == "" {
		return false
	}
	for _, arg := range args {
		trimmed := strings.ToLower(strings.TrimSpace(arg))
		if trimmed == normalizedFlag || strings.HasPrefix(trimmed, normalizedFlag+"=") {
			return true
		}
	}
	return false
}

func prependRequiredOptionArg(args []string, option, value string) []string {
	option = strings.TrimSpace(option)
	value = strings.TrimSpace(value)
	if option == "" || value == "" {
		return append([]string{}, args...)
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, option, value)
	for _, arg := range args {
		if strings.TrimSpace(arg) == value {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func mergeCommandContractRepairOutput(primary, repaired []byte, note string) []byte {
	if len(primary) == 0 {
		return append([]byte{}, repaired...)
	}
	if len(repaired) == 0 {
		return append([]byte{}, primary...)
	}
	note = strings.TrimSpace(note)
	const divider = "\n--- command contract repair retry ---\n"
	out := make([]byte, 0, len(primary)+len(repaired)+len(divider)+len(note)+2)
	out = append(out, primary...)
	out = append(out, []byte(divider)...)
	if note != "" {
		out = append(out, []byte(note)...)
		out = append(out, '\n')
	}
	out = append(out, repaired...)
	return out
}

func truncateStringBytes(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return strings.TrimSpace(value[:max])
}
