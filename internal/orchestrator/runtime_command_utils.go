package orchestrator

import (
	"path/filepath"
	"strings"
)

func applyCommandTargetFallback(scopePolicy *ScopePolicy, task TaskSpec, command string, args []string) ([]string, bool, string) {
	if scopePolicy == nil {
		return args, false, ""
	}
	command = strings.TrimSpace(command)
	if command == "" || !isNetworkSensitiveCommand(command) {
		return args, false, ""
	}
	if isMetasploitConsoleCommand(command) {
		return args, false, ""
	}
	if isNmapCommand(command) && nmapHasInputListArg(args) {
		return args, false, ""
	}
	if len(scopePolicy.extractTargets(command, args)) > 0 {
		return args, false, ""
	}
	fallback := firstTaskTarget(task.Targets)
	if fallback == "" {
		fallback = scopePolicy.FirstAllowedTarget()
	}
	if fallback == "" {
		return args, false, ""
	}
	updated := append(append([]string{}, args...), fallback)
	return updated, true, fallback
}

func nmapHasInputListArg(args []string) bool {
	for _, raw := range args {
		arg := strings.ToLower(strings.TrimSpace(raw))
		if arg == "-il" || strings.HasPrefix(arg, "-il") {
			return true
		}
	}
	return false
}

func isMetasploitConsoleCommand(command string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	return base == "msfconsole"
}

func normalizeMetasploitExecArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	out := append([]string{}, args...)
	for i := 0; i < len(out)-1; i++ {
		flag := strings.ToLower(strings.TrimSpace(out[i]))
		if flag != "-x" && flag != "--exec" && flag != "-e" {
			continue
		}
		payload := strings.TrimSpace(out[i+1])
		if payload == "" {
			continue
		}
		quote := byte(0)
		if strings.HasPrefix(payload, "\"") {
			quote = '"'
		} else if strings.HasPrefix(payload, "'") {
			quote = '\''
		}
		if quote != 0 {
			if strings.HasSuffix(payload, string(quote)) && len(payload) > 1 {
				out[i+1] = payload[1 : len(payload)-1]
				continue
			}
			joined := payload
			end := i + 1
			for j := i + 2; j < len(out); j++ {
				joined += " " + strings.TrimSpace(out[j])
				end = j
				if strings.HasSuffix(strings.TrimSpace(out[j]), string(quote)) {
					break
				}
			}
			trimmed := strings.TrimSpace(joined)
			if len(trimmed) >= 2 && trimmed[0] == quote && trimmed[len(trimmed)-1] == quote {
				out[i+1] = trimmed[1 : len(trimmed)-1]
				out = append(out[:i+2], out[end+1:]...)
				continue
			}
			// Keep payload executable even when quotes are malformed.
			out[i+1] = strings.TrimPrefix(trimmed, string(quote))
			out = append(out[:i+2], out[end+1:]...)
			continue
		}
		if len(payload) >= 2 && strings.HasSuffix(payload, "\"") && strings.HasPrefix(payload, "\"") {
			out[i+1] = payload[1 : len(payload)-1]
		}
		if len(payload) >= 2 && strings.HasSuffix(payload, "'") && strings.HasPrefix(payload, "'") {
			out[i+1] = payload[1 : len(payload)-1]
		}
	}
	return out
}

func enforceCommandExecutionMode(command string, args []string) (string, []string, bool) {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	if command == "" || !hasShellOperatorArgs(args) {
		return command, args, false
	}
	if isShellCommandBinary(command) {
		if len(args) > 0 {
			first := strings.ToLower(strings.TrimSpace(args[0]))
			if first == "-c" || first == "-lc" {
				return command, args, false
			}
		}
		script := buildShellScriptFromTokens(args)
		if strings.TrimSpace(script) == "" {
			return command, args, false
		}
		return command, []string{"-lc", script}, true
	}
	script := buildShellScriptFromTokens(append([]string{command}, args...))
	if strings.TrimSpace(script) == "" {
		return command, args, false
	}
	return "bash", []string{"-lc", script}, true
}

func hasShellOperatorArgs(args []string) bool {
	for _, raw := range args {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		if isShellOperatorToken(token) || isInlineShellRedirectToken(token) {
			return true
		}
	}
	return false
}

func isShellOperatorToken(token string) bool {
	switch strings.TrimSpace(token) {
	case "|", "||", "&&", ";", ">", ">>", "<", "<<", "<<<", "2>", "1>", "2>>", "1>>", "&>", "|&":
		return true
	default:
		return false
	}
}

func isInlineShellRedirectToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	inlinePrefixes := []string{"2>", "1>", "&>", "2>>", "1>>", ">>", ">", "<<", "<<<", "<"}
	for _, prefix := range inlinePrefixes {
		if strings.HasPrefix(token, prefix) && len(token) > len(prefix) {
			return true
		}
	}
	return false
}

func isShellCommandBinary(command string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "bash", "sh", "zsh":
		return true
	default:
		return false
	}
}

func buildShellScriptFromTokens(tokens []string) string {
	out := make([]string, 0, len(tokens))
	for _, raw := range tokens {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		if isShellOperatorToken(token) || isInlineShellRedirectToken(token) {
			out = append(out, token)
			continue
		}
		out = append(out, quoteShellToken(token))
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

func quoteShellToken(token string) string {
	if token == "" {
		return "''"
	}
	if !strings.ContainsAny(token, " \t\r\n'\"`$\\&;<>*?![]{}()") {
		return token
	}
	escaped := strings.ReplaceAll(token, `'`, `'"'"'`)
	return "'" + escaped + "'"
}

func firstTaskTarget(targets []string) string {
	for _, target := range targets {
		trimmed := strings.TrimSpace(target)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		out = append(out, arg)
	}
	return out
}
