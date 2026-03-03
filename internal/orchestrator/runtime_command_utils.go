package orchestrator

import "strings"

func applyCommandTargetFallback(scopePolicy *ScopePolicy, task TaskSpec, command string, args []string) ([]string, bool, string) {
	if scopePolicy == nil {
		return args, false, ""
	}
	command = strings.TrimSpace(command)
	if command == "" || !isNetworkSensitiveCommand(command) {
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
