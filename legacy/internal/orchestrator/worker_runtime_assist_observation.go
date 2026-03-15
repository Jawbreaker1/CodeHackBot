package orchestrator

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

func appendObservation(observations []string, entry string) []string {
	entry = normalizeObservationEntry(entry)
	if entry == "" {
		return observations
	}
	observations = append(observations, entry)
	return compactObservations(observations, workerAssistObsLimit, workerAssistObsTokenBudget)
}

var (
	obsErrorHintTerms = []string{
		"failed", "error", "timeout", "timed out", "denied", "not found", "no such file", "scope_denied", "invalid",
	}
	obsIPv4LikePattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?:/\d{1,2})?\b`)
	obsURLPattern      = regexp.MustCompile(`https?://[^\s]+`)
)

type observationItem struct {
	Index     int
	Entry     string
	Tokens    int
	HasPath   bool
	HasTarget bool
	HasError  bool
	Score     int
}

func compactObservations(observations []string, maxItems int, tokenBudget int) []string {
	normalized := make([]string, 0, len(observations))
	for _, entry := range observations {
		trimmed := normalizeObservationEntry(entry)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	if maxItems <= 0 {
		return []string{normalized[len(normalized)-1]}
	}
	if tokenBudget <= 0 {
		tokenBudget = workerAssistObsTokenBudget
	}

	items := make([]observationItem, 0, len(normalized))
	totalTokens := 0
	for idx, entry := range normalized {
		item := buildObservationItem(idx, entry)
		items = append(items, item)
		totalTokens += item.Tokens
	}
	if len(items) <= maxItems && totalTokens <= tokenBudget {
		return append([]string{}, normalized...)
	}

	selected := map[int]struct{}{}
	selectedTokens := 0
	selectIndex := func(idx int) {
		if _, exists := selected[idx]; exists {
			return
		}
		selected[idx] = struct{}{}
		selectedTokens += items[idx].Tokens
	}

	latestIndex := len(items) - 1
	selectIndex(latestIndex)
	minKeep := obsMinInt(4, maxItems)

	priorityOrder := make([]int, 0, len(items))
	for idx := range items {
		if idx == latestIndex {
			continue
		}
		priorityOrder = append(priorityOrder, idx)
	}
	sort.Slice(priorityOrder, func(i, j int) bool {
		left := items[priorityOrder[i]]
		right := items[priorityOrder[j]]
		if left.Score == right.Score {
			return left.Index > right.Index
		}
		return left.Score > right.Score
	})

	for _, idx := range priorityOrder {
		if len(selected) >= maxItems {
			break
		}
		item := items[idx]
		needsSignalRetention := item.Score >= 3
		if selectedTokens+item.Tokens > tokenBudget && len(selected) >= minKeep && !needsSignalRetention {
			continue
		}
		selectIndex(idx)
	}

	for idx := latestIndex - 1; idx >= 0 && len(selected) < maxItems; idx-- {
		if _, exists := selected[idx]; exists {
			continue
		}
		item := items[idx]
		if selectedTokens+item.Tokens > tokenBudget && len(selected) >= minKeep {
			continue
		}
		selectIndex(idx)
	}

	selectedIndices := make([]int, 0, len(selected))
	for idx := range selected {
		selectedIndices = append(selectedIndices, idx)
	}
	sort.Ints(selectedIndices)
	retained := make([]string, 0, len(selectedIndices))
	for _, idx := range selectedIndices {
		retained = append(retained, items[idx].Entry)
	}

	dropped := make([]string, 0, len(items)-len(selectedIndices))
	for idx, item := range items {
		if _, exists := selected[idx]; exists {
			continue
		}
		dropped = append(dropped, item.Entry)
	}
	if summary := buildObservationCompactionSummary(dropped); summary != "" {
		retained = prependObservationSummary(retained, summary, maxItems, tokenBudget)
	}
	return retained
}

func normalizeObservationEntry(entry string) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(entry)), " ")
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= workerAssistObsMaxEntryChars {
		return trimmed
	}
	head := (workerAssistObsMaxEntryChars * 2) / 3
	tail := workerAssistObsMaxEntryChars - head - 3
	if tail < 0 {
		tail = 0
	}
	if head > len(trimmed) {
		head = len(trimmed)
	}
	if tail > len(trimmed)-head {
		tail = len(trimmed) - head
	}
	if tail <= 0 {
		return trimmed[:head]
	}
	return trimmed[:head] + "..." + trimmed[len(trimmed)-tail:]
}

func buildObservationItem(index int, entry string) observationItem {
	hasPath := len(extractObservationPaths(entry)) > 0
	hasTarget := len(extractObservationTargets(entry)) > 0
	hasError := containsObservationErrorHint(entry)
	score := 0
	if hasPath {
		score += 4
	}
	if hasTarget {
		score += 4
	}
	if hasError {
		score += 4
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(entry)), "recovery:") {
		score++
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(entry)), "command failed:") {
		score++
	}
	return observationItem{
		Index:     index,
		Entry:     entry,
		Tokens:    estimateObservationTokens(entry),
		HasPath:   hasPath,
		HasTarget: hasTarget,
		HasError:  hasError,
		Score:     score,
	}
}

func estimateObservationTokens(entry string) int {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" {
		return 0
	}
	wordApprox := len(strings.Fields(trimmed))
	charApprox := (len(trimmed) + 3) / 4
	if charApprox > wordApprox {
		return charApprox
	}
	if wordApprox < 1 {
		return 1
	}
	return wordApprox
}

func containsObservationErrorHint(entry string) bool {
	lower := strings.ToLower(strings.TrimSpace(entry))
	if lower == "" {
		return false
	}
	for _, hint := range obsErrorHintTerms {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func extractObservationPaths(entry string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, token := range splitAnchorTokens(entry) {
		anchor := normalizePathAnchorToken(token)
		if anchor == "" {
			continue
		}
		if _, exists := seen[anchor]; exists {
			continue
		}
		seen[anchor] = struct{}{}
		out = append(out, anchor)
		if len(out) >= workerAssistObsCompactionAnchorCap {
			break
		}
	}
	return out
}

func extractObservationTargets(entry string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	add := func(value string) {
		trimmed := strings.TrimSpace(strings.Trim(value, "\"'()[]{}<>.,;:"))
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	for _, match := range obsURLPattern.FindAllString(entry, -1) {
		add(match)
		if len(out) >= workerAssistObsCompactionAnchorCap {
			return out
		}
	}
	for _, match := range obsIPv4LikePattern.FindAllString(entry, -1) {
		if strings.Contains(match, "/") {
			if _, _, err := net.ParseCIDR(match); err == nil {
				add(match)
			}
		} else if net.ParseIP(match) != nil {
			add(match)
		}
		if len(out) >= workerAssistObsCompactionAnchorCap {
			return out
		}
	}
	return out
}

func buildObservationCompactionSummary(dropped []string) string {
	if len(dropped) == 0 {
		return ""
	}
	paths := []string{}
	targets := []string{}
	errors := []string{}
	seenPath := map[string]struct{}{}
	seenTarget := map[string]struct{}{}
	seenErr := map[string]struct{}{}
	for _, entry := range dropped {
		for _, path := range extractObservationPaths(entry) {
			if _, exists := seenPath[path]; exists {
				continue
			}
			seenPath[path] = struct{}{}
			paths = append(paths, path)
			if len(paths) >= workerAssistObsCompactionAnchorCap {
				break
			}
		}
		for _, target := range extractObservationTargets(entry) {
			if _, exists := seenTarget[target]; exists {
				continue
			}
			seenTarget[target] = struct{}{}
			targets = append(targets, target)
			if len(targets) >= workerAssistObsCompactionAnchorCap {
				break
			}
		}
		if containsObservationErrorHint(entry) {
			errKey := summarizeObservationError(entry)
			if errKey != "" {
				if _, exists := seenErr[errKey]; !exists {
					seenErr[errKey] = struct{}{}
					errors = append(errors, errKey)
				}
			}
			if len(errors) >= workerAssistObsCompactionAnchorCap {
				break
			}
		}
	}
	parts := []string{fmt.Sprintf("compaction_summary: dropped=%d", len(dropped))}
	if len(paths) > 0 {
		parts = append(parts, "paths="+strings.Join(paths, ","))
	}
	if len(targets) > 0 {
		parts = append(parts, "targets="+strings.Join(targets, ","))
	}
	if len(errors) > 0 {
		parts = append(parts, "errors="+strings.Join(errors, ","))
	}
	if len(parts) <= 1 {
		return ""
	}
	return normalizeObservationEntry(strings.Join(parts, " | "))
}

func summarizeObservationError(entry string) string {
	normalized := normalizeObservationEntry(entry)
	if normalized == "" {
		return ""
	}
	if len(normalized) > workerAssistObsCompactionErrMaxChars {
		normalized = normalized[:workerAssistObsCompactionErrMaxChars] + "..."
	}
	return normalized
}

func prependObservationSummary(retained []string, summary string, maxItems int, tokenBudget int) []string {
	summary = normalizeObservationEntry(summary)
	if summary == "" {
		return retained
	}
	if tokenBudget <= 0 {
		tokenBudget = workerAssistObsTokenBudget
	}
	out := append([]string{}, retained...)
	if len(out) >= maxItems {
		dropIdx := observationDropIndexForSummary(out)
		if dropIdx < 0 {
			return out
		}
		next := make([]string, 0, len(out))
		next = append(next, out[:dropIdx]...)
		next = append(next, out[dropIdx+1:]...)
		out = next
		out = append([]string{summary}, out...)
	} else {
		out = append([]string{summary}, out...)
	}
	for observationTokens(out) > tokenBudget && len(out) > 1 {
		// Prefer dropping oldest non-summary entries first.
		if len(out) > 2 {
			out = append(out[:1], out[2:]...)
			continue
		}
		out = out[:1]
	}
	return out
}

func observationDropIndexForSummary(entries []string) int {
	if len(entries) == 0 {
		return -1
	}
	latest := len(entries) - 1
	dropIdx := -1
	dropScore := 1 << 30
	for idx, entry := range entries {
		if idx == latest {
			continue
		}
		score := buildObservationItem(idx, entry).Score
		if score < dropScore {
			dropScore = score
			dropIdx = idx
		}
	}
	if dropIdx >= 0 {
		return dropIdx
	}
	return 0
}

func observationTokens(values []string) int {
	total := 0
	for _, value := range values {
		total += estimateObservationTokens(value)
	}
	return total
}

func obsMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
