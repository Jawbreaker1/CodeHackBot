package assist

import (
	"strings"
	"unicode"
)

func extractJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimLeft(trimmed, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
		trimmed = strings.TrimSpace(trimmed)
	}
	if strings.HasSuffix(trimmed, "```") {
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	if obj, ok := extractJSONObject(trimmed); ok {
		return obj
	}
	return trimmed
}

func extractJSONObject(text string) (string, bool) {
	start := -1
	depth := 0
	inString := false
	escape := false
	for i, r := range text {
		if start == -1 {
			if r == '{' {
				start = i
				depth = 1
				inString = false
				escape = false
			}
			continue
		}
		if inString {
			if escape {
				escape = false
				continue
			}
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : i+1]), true
			}
		}
	}
	return "", false
}

func parseSimpleCommand(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if lower == "run:" || strings.HasPrefix(lower, "run ") {
			continue
		}
		if strings.HasPrefix(line, "```") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "<") || strings.Contains(strings.ToLower(line), "<channel") || strings.Contains(strings.ToLower(line), "<message") {
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			line = strings.TrimSpace(line[2:])
		}
		if looksLikeShellCommand(line) {
			return line
		}
	}
	return ""
}

func looksLikeShellCommand(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	first := line
	if idx := strings.IndexAny(line, " \t"); idx > 0 {
		first = line[:idx]
	}
	if first == "" || strings.HasPrefix(first, "<") {
		return false
	}
	for _, r := range first {
		if unicode.IsUpper(r) {
			return false
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '/', '.', '_', '-', ':':
			continue
		default:
			return false
		}
	}
	if strings.Contains(line, "json<message") {
		return false
	}
	if strings.Contains(line, " ") {
		return true
	}
	common := map[string]struct{}{
		"ls": {}, "pwd": {}, "whoami": {}, "hostname": {}, "ip": {}, "ifconfig": {}, "uname": {}, "cat": {}, "curl": {}, "nmap": {}, "dig": {}, "nslookup": {}, "whois": {},
	}
	if _, ok := common[line]; ok {
		return true
	}
	return false
}
