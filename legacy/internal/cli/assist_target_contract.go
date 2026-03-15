package cli

import (
	"fmt"
	"net"
	neturl "net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

var expectedAtTargetPattern = regexp.MustCompile(`(?i)\bexpected\s+at\s+([^\s,;:()]+)`)

func (r *Runner) enforceAssistTargetPivotContract(suggestion assist.Suggestion) error {
	if suggestion.Type != "command" {
		return nil
	}
	goal := strings.TrimSpace(r.assistRuntime.Goal)
	if goal == "" || !looksLikeAction(goal) {
		return nil
	}
	primaryTarget := primaryGoalTarget(goal)
	if primaryTarget == "" {
		return nil
	}
	targets := extractCommandTargets(suggestion.Command, suggestion.Args)
	if len(targets) == 0 {
		return nil
	}
	if targetMatchesPrimary(targets, primaryTarget) {
		return nil
	}
	decision := strings.ToLower(strings.TrimSpace(suggestion.Decision))
	if decision != "pivot_strategy" {
		return fmt.Errorf("assistant target contract: command target changed from %q to %q without decision=pivot_strategy", primaryTarget, targets[0])
	}
	if strings.TrimSpace(suggestion.Summary) == "" {
		return fmt.Errorf("assistant target contract: pivot_strategy requires a non-empty summary explaining why the pivot is needed")
	}
	return nil
}

func primaryGoalTarget(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	if match := expectedAtTargetPattern.FindStringSubmatch(goal); len(match) == 2 {
		if normalized := normalizeTargetToken(match[1]); normalized != "" {
			return normalized
		}
	}
	if token := extractHostLikeToken(goal); token != "" {
		return normalizeTargetToken(token)
	}
	return ""
}

func targetMatchesPrimary(targets []string, primary string) bool {
	primary = normalizeTargetToken(primary)
	if primary == "" {
		return true
	}
	primaryIP := net.ParseIP(primary)
	for _, target := range targets {
		target = normalizeTargetToken(target)
		if target == "" {
			continue
		}
		if strings.EqualFold(target, primary) {
			return true
		}
		if _, cidr, err := net.ParseCIDR(target); err == nil && primaryIP != nil && cidr.Contains(primaryIP) {
			return true
		}
	}
	return false
}

func extractCommandTargets(command string, args []string) []string {
	candidates := make([]string, 0, len(args)+1)
	seen := map[string]struct{}{}
	add := func(token string) {
		token = normalizeTargetToken(token)
		if token == "" {
			return
		}
		key := strings.ToLower(token)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, token)
	}
	add(command)
	for _, arg := range args {
		add(arg)
	}
	return candidates
}

func normalizeTargetToken(token string) string {
	token = strings.TrimSpace(strings.Trim(token, "\"'`(),;[]{}<>"))
	if token == "" {
		return ""
	}
	if strings.HasPrefix(token, "-") {
		return ""
	}
	if strings.Contains(token, "://") {
		parsed, err := neturl.Parse(token)
		if err != nil || parsed.Hostname() == "" {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	}
	if _, _, err := net.ParseCIDR(token); err == nil {
		return strings.ToLower(token)
	}
	if ip := net.ParseIP(token); ip != nil {
		return ip.String()
	}
	if strings.Contains(token, "/") || strings.Contains(token, "\\") {
		return ""
	}
	if isLikelyFilename(token) {
		return ""
	}
	if strings.Count(token, ".") >= 1 && !strings.HasPrefix(token, ".") && !strings.HasSuffix(token, ".") {
		return strings.ToLower(token)
	}
	return ""
}

func isLikelyFilename(token string) bool {
	ext := strings.ToLower(filepath.Ext(token))
	switch ext {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".zip", ".log", ".csv", ".xml", ".html", ".sh", ".py", ".go":
		return true
	default:
		return false
	}
}
