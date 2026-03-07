package cli

import (
	"fmt"
	"regexp"
	"strings"
)

var numberedListTokenPattern = regexp.MustCompile(`^[0-9]+[.)]$`)

func normalizeAssistCommandContract(command string, args []string) (string, []string, error) {
	command = strings.TrimSpace(command)
	args = trimAssistArgs(args)
	if command == "" {
		return "", nil, fmt.Errorf("assistant command contract: empty command")
	}

	// Models sometimes place a full shell line in `command`. Normalize into
	// command token + args to keep execution deterministic.
	if strings.ContainsAny(command, " \t\r\n") {
		parts := strings.Fields(command)
		if len(parts) == 0 {
			return "", nil, fmt.Errorf("assistant command contract: empty command")
		}
		command = parts[0]
		args = append(parts[1:], args...)
	}

	line := strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
	if looksLikeProseCommand(command, line) {
		return "", nil, fmt.Errorf("assistant command contract: non-executable prose command rejected")
	}

	// `/run` executes binaries directly and does not interpret shell operators.
	// Require explicit shell wrapper when operators are needed.
	if !isShellCommand(command) && hasShellOperator(args) {
		return "", nil, fmt.Errorf("assistant command contract: shell operators require explicit bash/sh -c wrapper")
	}

	return command, args, nil
}

func trimAssistArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func looksLikeProseCommand(command, line string) bool {
	token := strings.ToLower(strings.TrimSpace(command))
	if token == "" {
		return true
	}
	if isReservedContractToken(token) {
		return true
	}
	if strings.HasSuffix(token, ":") {
		return true
	}
	if numberedListTokenPattern.MatchString(token) {
		return true
	}
	switch token {
	case "first", "second", "third", "then", "next", "finally", "run", "execute", "step":
		return true
	}
	lowerLine := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.HasPrefix(lowerLine, "1. "),
		strings.HasPrefix(lowerLine, "2. "),
		strings.HasPrefix(lowerLine, "3. "),
		strings.HasPrefix(lowerLine, "first "),
		strings.HasPrefix(lowerLine, "then "),
		strings.HasPrefix(lowerLine, "next "),
		strings.HasPrefix(lowerLine, "run "),
		strings.HasPrefix(lowerLine, "execute "),
		strings.HasPrefix(lowerLine, "type: "),
		strings.Contains(lowerLine, " should be "),
		strings.Contains(lowerLine, " based on previous "),
		strings.Contains(lowerLine, " was run and "),
		strings.Contains(lowerLine, " and created "):
		return true
	}
	return false
}

func isReservedContractToken(token string) bool {
	switch token {
	case "type",
		"decision",
		"objective_met",
		"evidence_refs",
		"why_met",
		"summary",
		"final",
		"risk",
		"steps",
		"plan":
		return true
	default:
		return false
	}
}

func isShellCommand(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "bash", "sh", "zsh":
		return true
	default:
		return false
	}
}

func hasShellOperator(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "|", "||", "&&", ";", ">", ">>", "<", "<<", "<<<", "2>", "1>", "&>":
			return true
		}
	}
	return false
}
