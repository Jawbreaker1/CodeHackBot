package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

const (
	taskJournalFilename   = "task_journal.jsonl"
	resultSnapshotFilename = "result_snapshot.md"
)

type taskJournalEntry struct {
	Time         string   `json:"time"`
	Goal         string   `json:"goal,omitempty"`
	Step         int      `json:"step,omitempty"`
	Kind         string   `json:"kind"`
	Tool         string   `json:"tool,omitempty"`
	Args         []string `json:"args,omitempty"`
	Result       string   `json:"result,omitempty"`
	Decision     string   `json:"decision,omitempty"`
	ExitCode     int      `json:"exit_code,omitempty"`
	LogPath      string   `json:"log_path,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	Excerpt      string   `json:"excerpt,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

func assistArtifactsDir(sessionDir string) string {
	return filepath.Join(sessionDir, "artifacts", "assist")
}

func taskJournalPath(sessionDir string) string {
	return filepath.Join(assistArtifactsDir(sessionDir), taskJournalFilename)
}

func resultSnapshotPath(sessionDir string) string {
	return filepath.Join(assistArtifactsDir(sessionDir), resultSnapshotFilename)
}

func (r *Runner) currentAssistGoal() string {
	goal := strings.TrimSpace(r.assistRuntime.Goal)
	if goal != "" {
		return goal
	}
	return strings.TrimSpace(r.pendingAssistGoal)
}

func (r *Runner) appendTaskJournalEntry(sessionDir string, entry taskJournalEntry) error {
	entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	if strings.TrimSpace(entry.Goal) == "" {
		entry.Goal = r.currentAssistGoal()
	}
	entry.Goal = truncate(strings.TrimSpace(entry.Goal), 400)
	entry.Kind = strings.TrimSpace(entry.Kind)
	if entry.Kind == "" {
		entry.Kind = "step"
	}
	entry.Tool = strings.TrimSpace(entry.Tool)
	entry.Result = strings.TrimSpace(strings.ToLower(entry.Result))
	entry.Decision = strings.TrimSpace(strings.ToLower(entry.Decision))
	entry.LogPath = strings.TrimSpace(entry.LogPath)
	entry.Excerpt = truncate(strings.TrimSpace(entry.Excerpt), 500)
	entry.Notes = truncate(strings.TrimSpace(entry.Notes), 300)
	entry.EvidenceRefs = compactStrings(entry.EvidenceRefs)
	if entry.Step <= 0 && r.assistRuntime.Step > 0 {
		entry.Step = r.assistRuntime.Step
	}

	path := taskJournalPath(sessionDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(payload, '\n'))
	return err
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func readTaskJournalEntries(sessionDir string, max int) ([]taskJournalEntry, error) {
	path := taskJournalPath(sessionDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	entries := make([]taskJournalEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry taskJournalEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if max > 0 && len(entries) > max {
		entries = entries[len(entries)-max:]
	}
	return entries, nil
}

func (r *Runner) taskJournalSummary(sessionDir string, max int) string {
	entries, err := readTaskJournalEntries(sessionDir, max)
	if err != nil || len(entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		tool := strings.TrimSpace(entry.Tool)
		if tool == "" {
			tool = strings.TrimSpace(entry.Kind)
		}
		args := strings.TrimSpace(strings.Join(entry.Args, " "))
		line := fmt.Sprintf("[%s] %s", fallbackBlock(entry.Result), tool)
		if args != "" {
			line += " " + truncate(args, 100)
		}
		if entry.ExitCode != 0 {
			line += fmt.Sprintf(" (exit=%d)", entry.ExitCode)
		}
		if strings.TrimSpace(entry.LogPath) != "" {
			line += " log=" + entry.LogPath
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (r *Runner) refreshFocusFromRecentState(sessionDir string, goal string) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		goal = r.currentAssistGoal()
	}
	if goal == "" {
		return
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return
	}
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		return
	}
	var bullets []string
	bullets = append(bullets, "Goal: "+collapseWhitespace(truncate(goal, 320)))
	if r.assistObjectiveMet {
		bullets = append(bullets, "Status: objective met")
	} else {
		bullets = append(bullets, "Status: in progress")
	}
	if len(state.RecentObservations) > 0 {
		last := state.RecentObservations[len(state.RecentObservations)-1]
		cmdLine := strings.TrimSpace(strings.Join(append([]string{last.Command}, last.Args...), " "))
		if cmdLine != "" {
			status := "ok"
			if last.ExitCode != 0 {
				status = "failed"
			}
			bullets = append(bullets, fmt.Sprintf("Last action: %s -> %s (exit=%d)", truncate(cmdLine, 180), status, last.ExitCode))
		}
	}
	toolStatus := summarizeToolOutcomes(state.RecentObservations, 6)
	if toolStatus != "" {
		bullets = append(bullets, "Tools tried: "+toolStatus)
	}
	if evidence := latestEvidenceRefs(state.RecentObservations, 2); len(evidence) > 0 {
		bullets = append(bullets, "Latest evidence: "+strings.Join(evidence, "; "))
	}
	_ = memory.WriteFocus(artifacts.FocusPath, bullets)
	_ = r.appendAssistMemoryOp(sessionDir, assistMemoryOperation{
		Direction: "write",
		Component: "focus",
		Source:    "memory.focus",
		Path:      artifacts.FocusPath,
		Chars:     len(strings.Join(bullets, "\n")),
		Items:     len(bullets),
		Reason:    "refresh_focus_from_state",
	})
}

func summarizeToolOutcomes(observations []memory.Observation, maxTools int) string {
	if len(observations) == 0 || maxTools <= 0 {
		return ""
	}
	type stats struct {
		ok   int
		fail int
	}
	ordered := make([]string, 0, len(observations))
	byTool := map[string]stats{}
	for _, obs := range observations {
		tool := strings.TrimSpace(obs.Command)
		if tool == "" {
			continue
		}
		if _, exists := byTool[tool]; !exists {
			ordered = append(ordered, tool)
		}
		s := byTool[tool]
		if obs.ExitCode == 0 {
			s.ok++
		} else {
			s.fail++
		}
		byTool[tool] = s
	}
	if len(ordered) > maxTools {
		ordered = ordered[len(ordered)-maxTools:]
	}
	parts := make([]string, 0, len(ordered))
	for _, tool := range ordered {
		s := byTool[tool]
		parts = append(parts, fmt.Sprintf("%s(ok:%d fail:%d)", tool, s.ok, s.fail))
	}
	return strings.Join(parts, ", ")
}

func latestEvidenceRefs(observations []memory.Observation, max int) []string {
	if len(observations) == 0 || max <= 0 {
		return nil
	}
	out := make([]string, 0, max)
	seen := map[string]struct{}{}
	for i := len(observations) - 1; i >= 0; i-- {
		path := strings.TrimSpace(observations[i].LogPath)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
		if len(out) >= max {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (r *Runner) writeResultSnapshot(goal string, suggestion assist.Suggestion) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	resolved, _ := r.resolveCompletionEvidenceRefs(suggestion.EvidenceRefs)
	finalText := strings.TrimSpace(firstNonEmpty(suggestion.Final, suggestion.Summary))
	if finalText == "" {
		finalText = "(completed)"
	}
	status := "unknown"
	if suggestion.ObjectiveMet != nil {
		if *suggestion.ObjectiveMet {
			status = "met"
		} else {
			status = "not_met"
		}
	}
	var lines []string
	lines = append(lines,
		"# Result Snapshot",
		"",
		fmt.Sprintf("- Time: %s", time.Now().UTC().Format(time.RFC3339)),
		fmt.Sprintf("- Goal: %s", strings.TrimSpace(goal)),
		fmt.Sprintf("- Objective status: %s", status),
	)
	if why := strings.TrimSpace(suggestion.WhyMet); why != "" {
		lines = append(lines, fmt.Sprintf("- Why: %s", why))
	}
	lines = append(lines, "", "## Result", "", finalText, "")
	if len(suggestion.EvidenceRefs) > 0 {
		lines = append(lines, "## Evidence Refs", "")
		for _, ref := range compactStrings(suggestion.EvidenceRefs) {
			lines = append(lines, "- "+ref)
		}
		lines = append(lines, "")
	}
	if len(resolved) > 0 {
		lines = append(lines, "## Resolved Evidence", "")
		for _, path := range resolved {
			lines = append(lines, "- "+path)
		}
		lines = append(lines, "")
	}

	path := resultSnapshotPath(sessionDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	_ = r.appendAssistMemoryOp(sessionDir, assistMemoryOperation{
		Direction: "write",
		Component: "result_snapshot",
		Source:    "session.artifacts.assist",
		Path:      path,
		Chars:     len(strings.Join(lines, "\n")),
		Items:     1,
		Reason:    "completion_snapshot",
	})
	return nil
}
