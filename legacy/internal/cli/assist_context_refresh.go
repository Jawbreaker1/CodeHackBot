package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

// refreshAssistContextSummarySnapshot keeps summary/facts fresh for assist prompts
// without clearing recent logs/observations. It is generic and task-agnostic.
func (r *Runner) refreshAssistContextSummarySnapshot(sessionDir string, reason string) error {
	if strings.TrimSpace(sessionDir) == "" {
		return nil
	}
	if r.cfg.Permissions.Level == "readonly" {
		return nil
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return err
	}
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		return err
	}
	if state.StepsSinceSummary <= 0 {
		return nil
	}

	logPaths := state.RecentLogs
	if r.cfg.Context.MaxRecentOutputs > 0 && len(logPaths) > r.cfg.Context.MaxRecentOutputs {
		logPaths = logPaths[len(logPaths)-r.cfg.Context.MaxRecentOutputs:]
	}
	logSnippets := snapshotLogSnippets(logPaths, 2000)
	if obsSnippet := snapshotObservationSnippet(state.RecentObservations, 8); obsSnippet.Content != "" {
		logSnippets = append(logSnippets, obsSnippet)
	}

	existingSummary, _ := memory.ReadBullets(artifacts.SummaryPath)
	existingFacts, _ := memory.ReadBullets(artifacts.FactsPath)
	input := memory.SummaryInput{
		SessionID:       r.sessionID,
		Reason:          fallbackRefreshReason(reason),
		ExistingSummary: existingSummary,
		ExistingFacts:   existingFacts,
		FocusSnippet:    readFileTrimmed(artifacts.FocusPath),
		PlanSnippet:     readFileTrimmed(filepath.Join(sessionDir, r.cfg.Session.PlanFilename)),
		ChatHistory:     r.readChatHistory(artifacts.ChatPath),
		LogSnippets:     logSnippets,
	}
	output, err := (memory.FallbackSummarizer{}).Summarize(context.Background(), input)
	if err != nil {
		return err
	}
	if err := memory.WriteSummary(artifacts.SummaryPath, output.Summary); err != nil {
		return err
	}
	_ = r.appendAssistMemoryOp(sessionDir, assistMemoryOperation{
		Direction: "write",
		Component: "summary",
		Source:    "memory.summary",
		Path:      artifacts.SummaryPath,
		Chars:     len(strings.Join(output.Summary, "\n")),
		Items:     len(output.Summary),
		Reason:    "assist_context_refresh",
	})
	if err := memory.WriteKnownFacts(artifacts.FactsPath, output.Facts); err != nil {
		return err
	}
	_ = r.appendAssistMemoryOp(sessionDir, assistMemoryOperation{
		Direction: "write",
		Component: "known_facts",
		Source:    "memory.known_facts",
		Path:      artifacts.FactsPath,
		Chars:     len(strings.Join(output.Facts, "\n")),
		Items:     len(output.Facts),
		Reason:    "assist_context_refresh",
	})

	// Mark freshness while preserving rolling evidence windows.
	state.StepsSinceSummary = 0
	state.LastSummaryAt = time.Now().UTC().Format(time.RFC3339)
	state.LastSummaryHash = hashSummarySnapshot(output.Summary, output.Facts)
	if err := memory.SaveState(artifacts.StatePath, state); err != nil {
		return err
	}
	_ = r.appendAssistMemoryOp(sessionDir, assistMemoryOperation{
		Direction: "write",
		Component: "state",
		Source:    "memory.state",
		Path:      artifacts.StatePath,
		Items:     1,
		Reason:    "assist_context_refresh",
	})
	return nil
}

func snapshotLogSnippets(paths []string, maxBytes int) []memory.LogSnippet {
	if len(paths) == 0 {
		return nil
	}
	out := make([]memory.LogSnippet, 0, len(paths))
	for _, path := range paths {
		if shouldSkipAssistSummaryPath(path) {
			continue
		}
		text := readLogSnippet(path, maxBytes)
		if text == "" {
			continue
		}
		out = append(out, memory.LogSnippet{
			Path:    path,
			Content: text,
		})
	}
	return out
}

func shouldSkipAssistSummaryPath(path string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	switch base {
	case "", "chat.log":
		return true
	default:
		return false
	}
}

func snapshotObservationSnippet(observations []memory.Observation, max int) memory.LogSnippet {
	if len(observations) == 0 {
		return memory.LogSnippet{}
	}
	if max > 0 && len(observations) > max {
		observations = observations[len(observations)-max:]
	}
	builder := strings.Builder{}
	for _, obs := range observations {
		cmd := strings.TrimSpace(strings.Join(append([]string{obs.Command}, obs.Args...), " "))
		if cmd == "" {
			continue
		}
		builder.WriteString("$ " + cmd + "\n")
		if strings.TrimSpace(obs.Error) != "" {
			builder.WriteString("error: " + obs.Error + "\n")
		}
		if strings.TrimSpace(obs.OutputExcerpt) != "" {
			builder.WriteString(obs.OutputExcerpt + "\n")
		}
	}
	content := strings.TrimSpace(builder.String())
	if content == "" {
		return memory.LogSnippet{}
	}
	return memory.LogSnippet{
		Path:    "recent-observations",
		Content: content,
	}
}

func readLogSnippet(path string, maxBytes int) string {
	path = strings.TrimSpace(path)
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

func hashSummarySnapshot(summary []string, facts []string) string {
	joined := strings.Join(append(append([]string{}, summary...), facts...), "\n")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:])
}

func fallbackRefreshReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return "assist_context_refresh"
	}
	return fmt.Sprintf("assist_context_refresh:%s", trimmed)
}
