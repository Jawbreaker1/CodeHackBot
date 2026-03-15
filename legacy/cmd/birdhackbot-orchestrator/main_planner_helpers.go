package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/playbook"
)

func plannerPlaybookHints(goal string, constraints []string, cfg config.Config) (string, []string) {
	query := plannerPlaybookQuery(goal, constraints)
	if query == "" {
		return "", nil
	}
	maxEntries, maxLines := plannerPlaybookBounds(cfg)
	if maxEntries <= 0 || maxLines <= 0 {
		return "", nil
	}
	entries, err := playbook.Load(plannerPlaybookDir())
	if err != nil || len(entries) == 0 {
		return "", nil
	}
	matches := playbook.Match(entries, query, maxEntries)
	if len(matches) == 0 {
		return "", nil
	}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match.Name)
	}
	return strings.TrimSpace(playbook.Render(matches, maxLines)), compactStrings(names)
}

func plannerPlaybookQuery(goal string, constraints []string) string {
	parts := make([]string, 0, len(constraints)+1)
	if trimmed := normalizeGoal(goal); trimmed != "" {
		parts = append(parts, trimmed)
	}
	for _, constraint := range constraints {
		if trimmed := strings.TrimSpace(constraint); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}

func plannerPlaybookBounds(cfg config.Config) (int, int) {
	maxEntries := cfg.Context.PlaybookMax
	if maxEntries == 0 {
		return 0, 0
	}
	if maxEntries < 0 {
		maxEntries = defaultPlannerPlaybookMax
	}
	maxLines := cfg.Context.PlaybookLines
	if maxLines <= 0 {
		maxLines = defaultPlannerPlaybookLines
	}
	return maxEntries, maxLines
}

func plannerPlaybookDir() string {
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return filepath.Join("docs", "playbooks")
	}
	current := wd
	for {
		candidate := filepath.Join(current, "docs", "playbooks")
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return filepath.Join("docs", "playbooks")
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func detectWorkerConfigPath() string {
	if existing := strings.TrimSpace(os.Getenv("BIRDHACKBOT_CONFIG_PATH")); existing != "" {
		if _, err := os.Stat(existing); err == nil {
			return existing
		}
	}
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return ""
	}
	candidate := filepath.Join(wd, "config", "default.json")
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
}
