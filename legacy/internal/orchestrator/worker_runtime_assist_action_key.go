package orchestrator

import (
	"path/filepath"
	"sort"
	"strings"
)

func buildAssistActionKey(command string, args []string) string {
	canonicalCommand := canonicalAssistActionCommand(command)
	canonicalArgs := canonicalAssistActionArgs(canonicalCommand, args)
	parts := append([]string{canonicalCommand}, canonicalArgs...)
	return strings.Join(parts, "\x1f")
}

var assistActionCommandAliases = map[string]string{
	"dir":         "list_dir",
	"ls":          "list_dir",
	"list_dir":    "list_dir",
	"read":        "read_file",
	"read_file":   "read_file",
	"write":       "write_file",
	"write_file":  "write_file",
	"links":       "parse_links",
	"parse_links": "parse_links",
}

var assistActionOrderInsensitiveCommands = map[string]struct{}{
	"list_dir": {},
}

func canonicalAssistActionCommand(command string) string {
	normalized := strings.ToLower(strings.TrimSpace(command))
	if normalized == "" {
		return ""
	}
	normalized = filepath.Base(normalized)
	if alias, ok := assistActionCommandAliases[normalized]; ok {
		return alias
	}
	return normalized
}

func canonicalAssistActionArgs(command string, args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		trimmed := strings.ToLower(strings.TrimSpace(arg))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) <= 1 {
		return out
	}
	if isAssistShellWrapper(command) {
		return out
	}
	if _, ok := assistActionOrderInsensitiveCommands[command]; !ok {
		return out
	}
	sort.Strings(out)
	return out
}

func isAssistShellWrapper(command string) bool {
	switch strings.ToLower(filepath.Base(strings.TrimSpace(command))) {
	case "bash", "sh", "zsh", "ash", "dash", "ksh":
		return true
	default:
		return false
	}
}
