package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (r *Runner) toolsSummary(sessionDir string, max int) string {
	if max <= 0 {
		max = 10
	}
	manifestPath := filepath.Join(sessionDir, "artifacts", "tools", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return ""
	}
	var entries []toolManifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return ""
	}
	if len(entries) == 0 {
		return ""
	}

	// Keep the latest entry per tool name (stable, compact).
	latest := map[string]toolManifestEntry{}
	order := []string{}
	for _, e := range entries {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		if _, ok := latest[name]; !ok {
			order = append(order, name)
		}
		latest[name] = e
	}
	sort.Strings(order)

	lines := []string{}
	for _, name := range order {
		e := latest[name]
		run := strings.TrimSpace(e.Run)
		if run == "" {
			run = "(no run command recorded)"
		}
		lang := strings.TrimSpace(e.Language)
		if lang == "" {
			lang = "unknown"
		}
		purpose := strings.TrimSpace(e.Purpose)
		line := name + " (" + lang + "): " + run
		if purpose != "" {
			line += " // " + purpose
		}
		lines = append(lines, "- "+line)
		if len(lines) >= max {
			break
		}
	}
	return strings.Join(lines, "\n")
}
