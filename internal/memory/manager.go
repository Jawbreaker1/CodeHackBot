package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Manager struct {
	SessionDir         string
	LogDir             string
	PlanFilename       string
	LedgerFilename     string
	LedgerEnabled      bool
	MaxRecentOutputs   int
	SummarizeEvery     int
	SummarizeAtPercent int
	MaxLogBytes        int
	ChatHistoryLines   int
}

func (m Manager) Ensure() (Artifacts, error) {
	return EnsureArtifacts(m.SessionDir)
}

func (m Manager) RecordLog(logPath string) (State, error) {
	artifacts, err := EnsureArtifacts(m.SessionDir)
	if err != nil {
		return State{}, err
	}
	state, err := LoadState(artifacts.StatePath)
	if err != nil {
		return State{}, err
	}
	if logPath != "" {
		state.RecentLogs = appendUnique(state.RecentLogs, logPath)
		state.StepsSinceSummary++
		if m.MaxRecentOutputs > 0 && len(state.RecentLogs) > m.MaxRecentOutputs {
			state.RecentLogs = state.RecentLogs[len(state.RecentLogs)-m.MaxRecentOutputs:]
		}
	}
	if err := SaveState(artifacts.StatePath, state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (m Manager) ShouldSummarize(state State) bool {
	if m.SummarizeEvery > 0 && state.StepsSinceSummary >= m.SummarizeEvery {
		return true
	}
	if m.MaxRecentOutputs > 0 && m.SummarizeAtPercent > 0 {
		threshold := percentThreshold(m.MaxRecentOutputs, m.SummarizeAtPercent)
		if threshold > 0 && len(state.RecentLogs) >= threshold {
			return true
		}
	}
	return false
}

func (m Manager) Summarize(ctx context.Context, summarizer Summarizer, reason string) error {
	artifacts, err := EnsureArtifacts(m.SessionDir)
	if err != nil {
		return err
	}
	state, err := LoadState(artifacts.StatePath)
	if err != nil {
		return err
	}
	logPaths := state.RecentLogs
	if len(logPaths) == 0 {
		logPaths, _ = findRecentLogs(m.logDir(), m.MaxRecentOutputs)
	}

	existingSummary, _ := ReadBullets(artifacts.SummaryPath)
	existingFacts, _ := ReadBullets(artifacts.FactsPath)

	input := SummaryInput{
		SessionID:       filepath.Base(m.SessionDir),
		Reason:          reason,
		ExistingSummary: existingSummary,
		ExistingFacts:   existingFacts,
		LogSnippets:     readLogSnippets(logPaths, m.maxLogBytes()),
	}

	if m.PlanFilename != "" {
		planPath := filepath.Join(m.SessionDir, m.PlanFilename)
		input.PlanSnippet = readSnippet(planPath, m.maxLogBytes())
	}
	input.FocusSnippet = readSnippet(artifacts.FocusPath, m.maxLogBytes())
	if m.LedgerEnabled && m.LedgerFilename != "" {
		ledgerPath := filepath.Join(m.SessionDir, m.LedgerFilename)
		input.LedgerSnippet = readSnippet(ledgerPath, m.maxLogBytes())
	}
	if m.ChatHistoryLines > 0 {
		input.ChatHistory = readTailLines(artifacts.ChatPath, m.ChatHistoryLines, m.maxLogBytes())
	}

	output, err := summarizer.Summarize(ctx, input)
	if err != nil {
		return err
	}

	summaryLines := normalizeLines(output.Summary)
	factLines := normalizeLines(output.Facts)

	if err := WriteSummary(artifacts.SummaryPath, summaryLines); err != nil {
		return err
	}
	mergedFacts := mergeLines(existingFacts, factLines)
	if err := WriteKnownFacts(artifacts.FactsPath, mergedFacts); err != nil {
		return err
	}

	state.StepsSinceSummary = 0
	state.LastSummaryAt = time.Now().UTC().Format(time.RFC3339)
	state.LastSummaryHash = hashLines(append(summaryLines, mergedFacts...))
	state.RecentLogs = nil
	if err := SaveState(artifacts.StatePath, state); err != nil {
		return err
	}
	return nil
}

func (m Manager) logDir() string {
	if m.LogDir != "" {
		return m.LogDir
	}
	return filepath.Join(m.SessionDir, "logs")
}

func (m Manager) maxLogBytes() int {
	if m.MaxLogBytes > 0 {
		return m.MaxLogBytes
	}
	return 4000
}

func appendUnique(items []string, value string) []string {
	if value == "" {
		return items
	}
	if len(items) > 0 && items[len(items)-1] == value {
		return items
	}
	return append(items, value)
}

func percentThreshold(total, percent int) int {
	if total <= 0 || percent <= 0 {
		return 0
	}
	threshold := (total*percent + 99) / 100
	if threshold < 1 {
		return 1
	}
	return threshold
}

func hashLines(lines []string) string {
	joined := strings.Join(lines, "\n")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:])
}

func findRecentLogs(logDir string, max int) ([]string, error) {
	if logDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	type logEntry struct {
		path    string
		modTime time.Time
	}
	list := []logEntry{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		list = append(list, logEntry{
			path:    filepath.Join(logDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].modTime.After(list[j].modTime)
	})
	if max > 0 && len(list) > max {
		list = list[:max]
	}
	paths := make([]string, 0, len(list))
	for _, entry := range list {
		paths = append(paths, entry.path)
	}
	return paths, nil
}

func readSnippet(path string, maxBytes int) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	return strings.TrimSpace(string(data))
}

func readTailLines(path string, maxLines, maxBytes int) string {
	if path == "" || maxLines <= 0 {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	if len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	return strings.Join(out, "\n")
}

func readLogSnippets(paths []string, maxBytes int) []LogSnippet {
	snippets := []LogSnippet{}
	for _, path := range paths {
		content := readSnippet(path, maxBytes)
		if content == "" {
			continue
		}
		snippets = append(snippets, LogSnippet{
			Path:    path,
			Content: content,
		})
	}
	return snippets
}
