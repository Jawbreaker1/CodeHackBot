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
	RecoveryState []string
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

func buildWorkerAssistLayeredContext(cfg WorkerRunConfig, task TaskSpec, observations []string, recoverHint string, dependencyArtifacts []string, lastExecFeedback *assistExecutionFeedback) workerAssistLayeredContext {
	runPaths := BuildRunPaths(cfg.SessionsDir, cfg.RunID)
	knownFactsSelection := selectKnownFacts(filepath.Join(runPaths.MemoryDir, "known_facts.md"), 20)
	openUnknownsSelection := selectTail(parseMarkdownBullets(filepath.Join(runPaths.MemoryDir, "open_questions.md")), 12, "latest")
	taskContract := buildTaskContractLines(task)
	recoveryAnchors := buildRecoveryAnchorLines(taskContract, observations, recoverHint, dependencyArtifacts)
	actionsSelection := selectRelevantObservations(observations, recoverHint, 8)
	recentArtifactsSelection := listRelevantArtifactPaths(filepath.Join(runPaths.ArtifactDir, cfg.TaskID), 10, recoveryAnchors)
	dependencyArtifactsSelection := selectRelevantExistingArtifactPaths(dependencyArtifacts, 8, "anchors+latest_dependency", recoveryAnchors)
	recoveryState := buildRecoveryStateLines(task, recentArtifactsSelection.Retained, dependencyArtifactsSelection.Retained, lastExecFeedback)
	knownFacts := knownFactsSelection.Retained
	openUnknowns := openUnknownsSelection.Retained
	recentActions := actionsSelection.Retained
	recentArtifacts := recentArtifactsSelection.Retained
	recentDependencyArtifacts := dependencyArtifactsSelection.Retained
	compactionNotes := buildCompactionSummaryLines(knownFactsSelection, openUnknownsSelection, actionsSelection, recentArtifactsSelection, dependencyArtifactsSelection)
	compactionNotes = append(compactionNotes, readMemoryCompactionSummaryLines(filepath.Join(runPaths.MemoryDir, "context.json"))...)
	compactionNotes = dedupeStrings(compactionNotes)
	summary := fmt.Sprintf(
		"Task %s attempt %d layered context: facts=%d actions=%d artifacts=%d dependency_artifacts=%d unknowns=%d contract=%d compacted=%d",
		cfg.TaskID,
		cfg.Attempt,
		len(knownFacts),
		len(recentActions),
		len(recentArtifacts),
		len(recentDependencyArtifacts),
		len(openUnknowns),
		len(taskContract),
		len(compactionNotes),
	)
	chatHistory := renderLayerSection("task_contract", taskContract)
	if len(recoveryState) > 0 {
		chatHistory = appendPromptSection(chatHistory, renderLayerSection("recovery_state", recoveryState))
	}
	if len(recoveryAnchors) > 0 {
		chatHistory = appendPromptSection(chatHistory, renderLayerSection("recovery_anchors", recoveryAnchors))
	}
	chatHistory = appendPromptSection(chatHistory, renderLayerSection("recent_actions", recentActions))
	if len(openUnknowns) > 0 {
		chatHistory = appendPromptSection(chatHistory, renderLayerSection("open_unknowns", openUnknowns))
	}
	recentLog := renderLayerSection("recovery_state", recoveryState)
	recentLog = appendPromptSection(recentLog, renderLayerSection("recovery_anchors", recoveryAnchors))
	recentLog = appendPromptSection(recentLog, renderLayerSection("recent_artifacts", recentArtifacts))
	if len(recentDependencyArtifacts) > 0 {
		recentLog = appendPromptSection(recentLog, renderLayerSection("dependency_artifacts", recentDependencyArtifacts))
	}
	if strings.TrimSpace(recoverHint) != "" {
		recentLog = appendPromptSection(recentLog, renderLayerSection("recovery_context", []string{recoverHint}))
	}
	if len(compactionNotes) > 0 {
		recentLog = appendPromptSection(recentLog, renderLayerSection("compaction_summary", compactionNotes))
	}
	inventory := appendUnique(nil, recentDependencyArtifacts...)
	inventory = appendUnique(inventory, recentArtifacts...)
	return workerAssistLayeredContext{
		Summary:       summary,
		KnownFacts:    knownFacts,
		OpenUnknowns:  openUnknowns,
		RecentActions: recentActions,
		RecoveryState: recoveryState,
		RecentLog:     strings.TrimSpace(recentLog),
		ChatHistory:   strings.TrimSpace(chatHistory),
		Inventory:     strings.Join(inventory, "\n"),
	}
}

func buildRecoveryStateLines(task TaskSpec, recentArtifacts []string, dependencyArtifacts []string, lastExecFeedback *assistExecutionFeedback) []string {
	lines := []string{}
	if lastExecFeedback != nil {
		commandLine := strings.TrimSpace(strings.Join(append([]string{lastExecFeedback.Command}, lastExecFeedback.Args...), " "))
		if commandLine != "" {
			lines = append(lines, "previous_command: "+commandLine)
		}
		lines = append(lines, fmt.Sprintf("previous_exit_code: %d", lastExecFeedback.ExitCode))
		if trimmed := strings.TrimSpace(lastExecFeedback.ResultSummary); trimmed != "" {
			lines = append(lines, "previous_result_summary: "+singleLine(trimmed, 240))
		}
		if trimmed := strings.TrimSpace(lastExecFeedback.RunError); trimmed != "" {
			lines = append(lines, "previous_runtime_error: "+trimmed)
		}
		if refs := buildPrimaryEvidenceRefs(*lastExecFeedback); len(refs) > 0 {
			lines = append(lines, "primary_evidence_refs: "+strings.Join(refs, ", "))
		}
		if trimmed := strings.TrimSpace(lastExecFeedback.CombinedOutputTail); trimmed != "" {
			lines = append(lines, "previous_output_tail: "+singleLine(trimmed, 240))
		}
	}
	if unmet := buildUnmetContractLines(task, recentArtifacts, dependencyArtifacts); len(unmet) > 0 {
		lines = append(lines, unmet...)
	}
	return appendUnique(nil, compactStrings(lines)...)
}

func buildPrimaryEvidenceRefs(feedback assistExecutionFeedback) []string {
	refs := []string{}
	refs = append(refs, feedback.PrimaryArtifactRefs...)
	if trimmed := strings.TrimSpace(feedback.LogPath); trimmed != "" {
		refs = append(refs, trimmed)
	}
	return appendUnique(nil, compactStrings(refs)...)
}

func buildUnmetContractLines(task TaskSpec, recentArtifacts []string, dependencyArtifacts []string) []string {
	if len(task.ExpectedArtifacts) == 0 {
		return nil
	}
	present := map[string]struct{}{}
	for _, artifact := range append(append([]string{}, recentArtifacts...), dependencyArtifacts...) {
		base := strings.TrimSpace(filepath.Base(artifact))
		if base != "" {
			present[strings.ToLower(base)] = struct{}{}
		}
	}
	missing := []string{}
	for _, raw := range task.ExpectedArtifacts {
		expected := strings.TrimSpace(filepath.Base(raw))
		if expected == "" {
			continue
		}
		if _, ok := present[strings.ToLower(expected)]; ok {
			continue
		}
		missing = append(missing, expected)
	}
	if len(missing) == 0 {
		return []string{"contract_gap: expected_artifacts_present"}
	}
	return []string{"contract_gap: missing_expected_artifacts=" + strings.Join(appendUnique(nil, missing...), ", ")}
}

func singleLine(value string, max int) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if max > 0 && len(value) > max {
		return value[:max] + "..."
	}
	return value
}

func buildTaskContractLines(task TaskSpec) []string {
	lines := []string{}
	if goal := strings.TrimSpace(task.Goal); goal != "" {
		lines = append(lines, "goal: "+goal)
	}
	if len(task.DoneWhen) > 0 {
		lines = append(lines, "done_when: "+strings.Join(compactStrings(task.DoneWhen), "; "))
	}
	if len(task.FailWhen) > 0 {
		lines = append(lines, "fail_when: "+strings.Join(compactStrings(task.FailWhen), "; "))
	}
	if len(task.ExpectedArtifacts) > 0 {
		lines = append(lines, "expected_artifacts: "+strings.Join(compactStrings(task.ExpectedArtifacts), ", "))
	}
	return lines
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

type rankedStringItem struct {
	value string
	score int
	order int
}

func selectRelevantObservations(observations []string, recoverHint string, max int) layeredSelection {
	all := trimmedNonEmptyLines(observations)
	total := len(all)
	if max <= 0 || total == 0 {
		return layeredSelection{Retained: nil, Total: total, Dropped: total, Policy: "anchors+latest_relevant"}
	}
	anchors := collectRecoveryAnchors(recoverHint)
	ranked := make([]rankedStringItem, 0, total)
	for i, item := range all {
		score := observationRelevanceScore(item, anchors, i, total)
		ranked = append(ranked, rankedStringItem{value: item, score: score, order: i})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].order > ranked[j].order
		}
		return ranked[i].score > ranked[j].score
	})
	if len(ranked) > max {
		ranked = ranked[:max]
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].order < ranked[j].order
	})
	retained := make([]string, 0, len(ranked))
	for _, item := range ranked {
		retained = append(retained, item.value)
	}
	return layeredSelection{
		Retained: retained,
		Total:    total,
		Dropped:  total - len(retained),
		Policy:   "anchors+latest_relevant",
	}
}

func observationRelevanceScore(item string, anchors []string, index, total int) int {
	trimmed := strings.TrimSpace(item)
	lower := strings.ToLower(trimmed)
	score := index
	if total > 0 {
		score += (index * 100) / total
	}
	if strings.HasPrefix(lower, "attempt delta summary") || strings.Contains(lower, "attempt_delta_summary") {
		score += 900
	}
	if strings.HasPrefix(lower, "carryover:") {
		score += 700
	}
	if containsAnySubstring(lower,
		"no progress",
		"budget exhausted",
		"scope denied",
		"missing_required_artifacts",
		"not found",
		"no such file",
		"permission denied",
		"timed out",
		"timeout",
		"failed",
		"error",
	) {
		score += 600
	}
	if containsAnySubstring(lower,
		"runtime input repair",
		"recover pivot accepted",
		"latest_execution_feedback",
		"command failed:",
		"command ok:",
	) {
		score += 300
	}
	if hasAnchorMatch(trimmed, anchors) {
		score += 500
	}
	return score
}

func listRelevantArtifactPaths(dir string, max int, anchors []string) layeredSelection {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return layeredSelection{Policy: "anchors+latest_relevant"}
	}
	type item struct {
		path  string
		ts    int64
		score int
	}
	items := make([]item, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || strings.HasPrefix(name, "context_envelope") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, name)
		score := artifactPathRelevanceScore(path, info.ModTime().UnixNano(), anchors)
		items = append(items, item{path: path, ts: info.ModTime().UnixNano(), score: score})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			if items[i].ts == items[j].ts {
				return items[i].path > items[j].path
			}
			return items[i].ts > items[j].ts
		}
		return items[i].score > items[j].score
	})
	total := len(items)
	if max <= 0 {
		return layeredSelection{Retained: nil, Total: total, Dropped: total, Policy: "anchors+latest_relevant"}
	}
	if total > max {
		items = items[:max]
	}
	retained := make([]string, 0, len(items))
	for _, item := range items {
		retained = append(retained, item.path)
	}
	return layeredSelection{
		Retained: retained,
		Total:    total,
		Dropped:  total - len(retained),
		Policy:   "anchors+latest_relevant",
	}
}

func selectRelevantExistingArtifactPaths(paths []string, max int, policy string, anchors []string) layeredSelection {
	type item struct {
		path  string
		ts    int64
		score int
	}
	seen := map[string]struct{}{}
	items := make([]item, 0, len(paths))
	for _, raw := range paths {
		candidate := filepath.Clean(strings.TrimSpace(raw))
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		items = append(items, item{
			path:  candidate,
			ts:    info.ModTime().UnixNano(),
			score: artifactPathRelevanceScore(candidate, info.ModTime().UnixNano(), anchors),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			if items[i].ts == items[j].ts {
				return items[i].path > items[j].path
			}
			return items[i].ts > items[j].ts
		}
		return items[i].score > items[j].score
	})
	total := len(items)
	if max <= 0 {
		return layeredSelection{
			Retained: nil,
			Total:    total,
			Dropped:  total,
			Policy:   policy,
		}
	}
	if total > max {
		items = items[:max]
	}
	retained := make([]string, 0, len(items))
	for _, item := range items {
		retained = append(retained, item.path)
	}
	return layeredSelection{
		Retained: retained,
		Total:    total,
		Dropped:  total - len(retained),
		Policy:   policy,
	}
}

func buildRecoveryAnchorLines(taskContract, observations []string, recoverHint string, dependencyArtifacts []string) []string {
	lines := []string{}
	if trimmed := strings.TrimSpace(recoverHint); trimmed != "" {
		lines = append(lines, "recover_hint: "+trimmed)
	}
	for _, entry := range selectRelevantObservations(observations, recoverHint, 4).Retained {
		lines = append(lines, "observation: "+strings.TrimSpace(entry))
	}
	for _, entry := range taskContract {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(entry)), "goal:") ||
			strings.HasPrefix(strings.ToLower(strings.TrimSpace(entry)), "expected_artifacts:") {
			lines = append(lines, "contract: "+entry)
		}
	}
	anchors := collectRecoveryAnchors(append([]string{recoverHint}, observations...)...)
	for _, artifact := range selectRelevantExistingArtifactPaths(dependencyArtifacts, 4, "anchors+latest_dependency", anchors).Retained {
		lines = append(lines, "dependency: "+artifact)
	}
	return appendUnique(nil, compactStrings(lines)...)
}

func collectRecoveryAnchors(values ...string) []string {
	out := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if anchor := extractRecoverEvidenceAnchor(trimmed); anchor != "" {
			out = append(out, anchor)
		}
		if token := firstPathLikeToken(trimmed); token != "" {
			out = append(out, token)
			out = append(out, filepath.Base(token))
		}
	}
	return appendUnique(nil, compactStrings(out)...)
}

func hasAnchorMatch(text string, anchors []string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, anchor := range anchors {
		trimmed := strings.TrimSpace(anchor)
		if trimmed == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(trimmed)) {
			return true
		}
		base := filepath.Base(trimmed)
		if base != "." && base != "" && strings.Contains(lower, strings.ToLower(base)) {
			return true
		}
	}
	return false
}

func artifactPathRelevanceScore(path string, ts int64, anchors []string) int {
	base := strings.ToLower(filepath.Base(path))
	score := 0
	if hasAnchorMatch(path, anchors) {
		score += 1000
	}
	if isSemanticEvidenceArtifact(base) {
		score += 400
	}
	if isInspectionChurnArtifact(base) {
		score -= 250
	}
	score += int((ts / 1_000_000) % 1000)
	return score
}

func isInspectionChurnArtifact(base string) bool {
	base = strings.ToLower(strings.TrimSpace(base))
	return strings.HasPrefix(base, "worker-") && strings.HasSuffix(base, ".log")
}

func isSemanticEvidenceArtifact(base string) bool {
	base = strings.ToLower(strings.TrimSpace(base))
	if isInspectionChurnArtifact(base) {
		return false
	}
	return containsAnySubstring(base,
		"report",
		"hash",
		"scan",
		"metadata",
		"attempt",
		"result",
		"summary",
		"secret",
		"password",
		"extract",
		"proof",
		"vuln",
		"service",
	)
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
	names := []string{"known_facts", "open_unknowns", "recent_actions", "recent_artifacts", "dependency_artifacts"}
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
