package cli

import (
	"fmt"
	"strings"
)

func assistInteractiveCommandReason(command string, args []string) (string, bool) {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return "", false
	}
	args = trimAssistArgs(args)

	if script, ok := extractShellScript(command, args); ok {
		if reason := interactiveShellScriptReason(script); reason != "" {
			return reason, true
		}
		return "", false
	}

	switch command {
	case "bash", "sh", "zsh":
		// Non-script shell launch is always interactive.
		return fmt.Sprintf("%s without -c/-lc script opens an interactive shell", command), true
	case "sudo", "su", "passwd":
		return fmt.Sprintf("%s may require interactive authentication input", command), true
	case "ssh", "sftp", "ftp", "telnet":
		return fmt.Sprintf("%s starts an interactive remote session", command), true
	case "mysql", "psql", "sqlite3":
		if !hasAnyArgPrefix(args, "-e", "-c", "--execute") {
			return fmt.Sprintf("%s without inline query enters interactive REPL", command), true
		}
	case "python", "python3":
		if !hasAnyArgPrefix(args, "-c", "-m") && !hasScriptPathArg(args) {
			return fmt.Sprintf("%s without script/-c enters interactive REPL", command), true
		}
	case "unzip":
		if hasArg(args, "-p") && !hasPasswordArg(args) {
			return "unzip -p without -P will block on password prompt", true
		}
		if !hasPasswordArg(args) && !hasAnyArg(args, "-l", "-t", "-v", "-z", "-Z") {
			return "unzip extraction without -P may block on password/overwrite prompts", true
		}
	case "less", "more", "man", "vi", "vim", "nano", "top", "htop", "watch", "tmux", "screen":
		return fmt.Sprintf("%s is interactive and blocks autonomous execution", command), true
	}
	return "", false
}

func interactiveShellScriptReason(script string) string {
	segments := splitShellSegments(script)
	for _, segment := range segments {
		parts := strings.Fields(segment)
		if len(parts) == 0 {
			continue
		}
		if reason, blocked := assistInteractiveCommandReason(parts[0], parts[1:]); blocked {
			return fmt.Sprintf("shell script contains interactive command `%s`: %s", parts[0], reason)
		}
	}
	return ""
}

func splitShellSegments(script string) []string {
	if strings.TrimSpace(script) == "" {
		return nil
	}
	return strings.FieldsFunc(script, func(r rune) bool {
		switch r {
		case ';', '|', '&', '\n':
			return true
		default:
			return false
		}
	})
}

func hasAnyArg(args []string, options ...string) bool {
	for _, arg := range args {
		for _, option := range options {
			if strings.EqualFold(strings.TrimSpace(arg), option) {
				return true
			}
		}
	}
	return false
}

func hasArg(args []string, option string) bool {
	return hasAnyArg(args, option)
}

func hasAnyArgPrefix(args []string, prefixes ...string) bool {
	for _, arg := range args {
		trimmed := strings.ToLower(strings.TrimSpace(arg))
		for _, prefix := range prefixes {
			if strings.HasPrefix(trimmed, strings.ToLower(prefix)) {
				return true
			}
		}
	}
	return false
}

func hasScriptPathArg(args []string) bool {
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" || strings.HasPrefix(trimmed, "-") {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasSuffix(lower, ".py") {
			return true
		}
	}
	return false
}

func hasPasswordArg(args []string) bool {
	for i, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "-P" {
			if i+1 < len(args) && strings.TrimSpace(args[i+1]) != "" {
				return true
			}
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "-p") && len(trimmed) > 2 {
			return true
		}
	}
	return false
}
