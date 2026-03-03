package orchestrator

import (
	"path/filepath"
	"strings"
)

func requiresCommandScopeValidation(command string, args []string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	if base != "bash" && base != "sh" && base != "zsh" {
		return true
	}
	if len(args) < 2 {
		return true
	}
	mode := strings.TrimSpace(args[0])
	if mode != "-c" && mode != "-lc" {
		return true
	}
	body := strings.TrimSpace(args[1])
	if body == "" {
		return true
	}
	// Conservative fail-closed: uncertain shell expansions still require scope validation.
	if strings.Contains(body, "$(") || strings.Contains(body, "`") {
		return true
	}
	for _, token := range shellCommandTokens(body) {
		if isNetworkSensitiveCommand(token) {
			return true
		}
	}
	return false
}

func shellCommandTokens(body string) []string {
	segments := strings.FieldsFunc(body, func(r rune) bool {
		switch r {
		case '|', ';', '&', '\n':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		fields := strings.Fields(strings.TrimSpace(segment))
		if len(fields) == 0 {
			continue
		}
		out = append(out, fields[0])
	}
	return out
}
