package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type workerAssistLayeredContext struct {
	Summary       string
	KnownFacts    []string
	OpenUnknowns  []string
	RecentActions []string
	RecentLog     string
	ChatHistory   string
	Inventory     string
}

type layeredSelection struct {
	Retained []string
	Total    int
	Dropped  int
	Policy   string
}

func buildWorkerAssistLayeredContext(cfg WorkerRunConfig, observations []string, recoverHint string) workerAssistLayeredContext {
	runPaths := BuildRunPaths(cfg.SessionsDir, cfg.RunID)
	knownFactsSelection := selectKnownFacts(filepath.Join(runPaths.MemoryDir, "known_facts.md"), 20)
	openUnknownsSelection := selectTail(parseMarkdownBullets(filepath.Join(runPaths.MemoryDir, "open_questions.md")), 12, "latest")
	actionsSelection := selectTail(trimmedNonEmptyLines(observations), 8, "latest")
	recentArtifactsSelection := listRecentArtifactPaths(filepath.Join(runPaths.ArtifactDir, cfg.TaskID), 10)
	knownFacts := knownFactsSelection.Retained
	openUnknowns := openUnknownsSelection.Retained
	recentActions := actionsSelection.Retained
	recentArtifacts := recentArtifactsSelection.Retained
	compactionNotes := buildCompactionSummaryLines(knownFactsSelection, openUnknownsSelection, actionsSelection, recentArtifactsSelection)
	compactionNotes = append(compactionNotes, readMemoryCompactionSummaryLines(filepath.Join(runPaths.MemoryDir, "context.json"))...)
	compactionNotes = dedupeStrings(compactionNotes)
	summary := fmt.Sprintf(
		"Task %s attempt %d layered context: facts=%d actions=%d artifacts=%d unknowns=%d compacted=%d",
		cfg.TaskID,
		cfg.Attempt,
		len(knownFacts),
		len(recentActions),
		len(recentArtifacts),
		len(openUnknowns),
		len(compactionNotes),
	)
	chatHistory := renderLayerSection("recent_actions", recentActions)
	if len(openUnknowns) > 0 {
		chatHistory += "\n" + renderLayerSection("open_unknowns", openUnknowns)
	}
	recentLog := renderLayerSection("recent_artifacts", recentArtifacts)
	if strings.TrimSpace(recoverHint) != "" {
		recentLog += "\n" + renderLayerSection("recovery_context", []string{recoverHint})
	}
	if len(compactionNotes) > 0 {
		recentLog += "\n" + renderLayerSection("compaction_summary", compactionNotes)
	}
	return workerAssistLayeredContext{
		Summary:       summary,
		KnownFacts:    knownFacts,
		OpenUnknowns:  openUnknowns,
		RecentActions: recentActions,
		RecentLog:     strings.TrimSpace(recentLog),
		ChatHistory:   strings.TrimSpace(chatHistory),
		Inventory:     strings.Join(recentArtifacts, "\n"),
	}
}

func readMarkdownBullets(path string, max int) []string {
	items := parseMarkdownBullets(path)
	if max > 0 && len(items) > max {
		items = items[:max]
	}
	return items
}

func parseMarkdownBullets(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		if item == "" {
			continue
		}
		if len(item) > 280 {
			item = item[:280] + "..."
		}
		out = append(out, item)
	}
	return out
}

func selectKnownFacts(path string, max int) layeredSelection {
	all := parseMarkdownBullets(path)
	anchors := make([]string, 0, len(all))
	dynamic := make([]string, 0, len(all))
	for _, item := range all {
		if isKnownFactAnchor(item) {
			anchors = append(anchors, item)
			continue
		}
		dynamic = append(dynamic, item)
	}
	if max <= 0 {
		return layeredSelection{
			Retained: nil,
			Total:    len(all),
			Dropped:  len(all),
			Policy:   "anchors+latest_dynamic",
		}
	}
	if len(anchors) > max {
		anchors = anchors[:max]
	}
	dynamicCap := max - len(anchors)
	dynamicSelection := selectTail(dynamic, dynamicCap, "latest")
	retained := append([]string{}, anchors...)
	retained = append(retained, dynamicSelection.Retained...)
	return layeredSelection{
		Retained: retained,
		Total:    len(all),
		Dropped:  len(all) - len(retained),
		Policy:   "anchors+latest_dynamic",
	}
}

func isKnownFactAnchor(item string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(item))
	return strings.HasPrefix(trimmed, "goal:") || strings.HasPrefix(trimmed, "planner decision:")
}

func listRecentArtifactPaths(dir string, max int) layeredSelection {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return layeredSelection{Policy: "latest"}
	}
	type item struct {
		path string
		ts   int64
	}
	items := make([]item, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "context_envelope") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, item{
			path: filepath.Join(dir, name),
			ts:   info.ModTime().UnixNano(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ts == items[j].ts {
			return items[i].path > items[j].path
		}
		return items[i].ts > items[j].ts
	})
	total := len(items)
	if max <= 0 {
		return layeredSelection{
			Retained: nil,
			Total:    total,
			Dropped:  total,
			Policy:   "latest",
		}
	}
	if total > max {
		items = items[:max]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.path)
	}
	return layeredSelection{
		Retained: out,
		Total:    total,
		Dropped:  total - len(out),
		Policy:   "latest",
	}
}

func renderLayerSection(name string, lines []string) string {
	name = strings.TrimSpace(name)
	if name == "" || len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[" + name + "]\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		b.WriteString("- " + trimmed + "\n")
	}
	return strings.TrimSpace(b.String())
}

func trimmedNonEmptyLines(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func tailStringSlice(values []string, max int) []string {
	if max <= 0 || len(values) <= max {
		return append([]string{}, values...)
	}
	return append([]string{}, values[len(values)-max:]...)
}

func selectTail(values []string, max int, policy string) layeredSelection {
	if policy == "" {
		policy = "latest"
	}
	total := len(values)
	retained := tailStringSlice(values, max)
	return layeredSelection{
		Retained: retained,
		Total:    total,
		Dropped:  total - len(retained),
		Policy:   policy,
	}
}

func buildCompactionSummaryLines(selections ...layeredSelection) []string {
	names := []string{"known_facts", "open_unknowns", "recent_actions", "recent_artifacts"}
	out := make([]string, 0, len(selections))
	for i, selection := range selections {
		if selection.Dropped <= 0 {
			continue
		}
		name := fmt.Sprintf("layer_%d", i)
		if i < len(names) {
			name = names[i]
		}
		policy := strings.TrimSpace(selection.Policy)
		if policy == "" {
			policy = "unspecified"
		}
		out = append(out, fmt.Sprintf("%s compacted: retained=%d/%d dropped=%d policy=%s", name, len(selection.Retained), selection.Total, selection.Dropped, policy))
	}
	return out
}

func readMemoryCompactionSummaryLines(path string) []string {
	ctx, err := readMemoryContext(path)
	if err != nil {
		return nil
	}
	lines := []string{}
	if ctx.KnownFactsDropped > 0 {
		lines = append(lines, fmt.Sprintf("memory_bank known_facts compacted: retained=%d/%d dropped=%d policy=anchors+latest_dynamic", maxInt(0, ctx.KnownFactsRetained), maxInt(0, ctx.KnownFactsCount), maxInt(0, ctx.KnownFactsDropped)))
	}
	if ctx.OpenQuestionsDropped > 0 {
		lines = append(lines, fmt.Sprintf("memory_bank open_unknowns compacted: retained=%d/%d dropped=%d policy=latest", maxInt(0, ctx.OpenQuestionsRetained), maxInt(0, ctx.OpenQuestionsCount), maxInt(0, ctx.OpenQuestionsDropped)))
	}
	return lines
}
