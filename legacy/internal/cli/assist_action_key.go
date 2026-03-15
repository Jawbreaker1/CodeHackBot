package cli

import (
	"path/filepath"
	"strings"
)

func canonicalAssistActionKey(command string, args []string) string {
	cmd := strings.ToLower(strings.TrimSpace(command))
	switch cmd {
	case "ls", "list_dir":
		target := strings.TrimSpace(listDirTarget(args))
		return "list_dir" + "\x1f" + canonicalPathToken(target)
	case "read", "read_file":
		target := "."
		for _, arg := range args {
			a := strings.TrimSpace(arg)
			if a == "" || isFlagLike(a) {
				continue
			}
			target = a
			break
		}
		return "read_file" + "\x1f" + canonicalPathToken(target)
	case "write", "write_file":
		target := "."
		if len(args) > 0 {
			target = strings.TrimSpace(args[0])
		}
		return "write_file" + "\x1f" + canonicalPathToken(target)
	case "browse":
		target := ""
		for _, arg := range args {
			a := strings.TrimSpace(arg)
			if a == "" || isFlagLike(a) {
				continue
			}
			target = a
			break
		}
		if normalized, err := normalizeURL(target); err == nil {
			target = normalized
		}
		return "browse" + "\x1f" + strings.ToLower(strings.TrimSpace(target))
	case "links", "parse_links":
		target := ""
		base := ""
		for _, arg := range args {
			a := strings.TrimSpace(arg)
			if a == "" {
				continue
			}
			lower := strings.ToLower(a)
			if strings.HasPrefix(lower, "base=") {
				base = strings.TrimSpace(a[len("base="):])
				continue
			}
			if target == "" {
				target = a
			}
		}
		if normalized, err := normalizeURL(base); err == nil {
			base = normalized
		}
		return "parse_links" + "\x1f" + canonicalPathToken(target) + "\x1f" + strings.ToLower(strings.TrimSpace(base))
	case "bash", "sh", "zsh":
		if script, ok := extractShellScript(command, args); ok {
			if key, matched := canonicalizeSimpleShellScript(script); matched {
				return key
			}
		}
	}

	normalizedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		a := strings.ToLower(strings.TrimSpace(arg))
		if a == "" {
			continue
		}
		normalizedArgs = append(normalizedArgs, a)
	}
	if len(normalizedArgs) == 0 {
		return cmd
	}
	return cmd + "\x1f" + strings.Join(normalizedArgs, "\x1f")
}

func canonicalPathToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "."
	}
	clean := filepath.Clean(raw)
	if clean == "." || clean == "/" {
		return clean
	}
	return strings.ToLower(clean)
}

func canonicalizeSimpleShellScript(script string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(script, ";"))
	if trimmed == "" {
		return "", false
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "", false
	}
	switch strings.ToLower(parts[0]) {
	case "ls":
		target := listDirTarget(parts[1:])
		return "list_dir" + "\x1f" + canonicalPathToken(target), true
	case "cat":
		target := "."
		if len(parts) > 1 {
			target = parts[1]
		}
		return "read_file" + "\x1f" + canonicalPathToken(target), true
	default:
		return "", false
	}
}
