package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
	"github.com/Jawbreaker1/CodeHackBot/internal/playbook"
)

func readFileTrimmed(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (r *Runner) assistInput(sessionDir, goal, mode string) (assist.Input, error) {
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return assist.Input{}, err
	}
	summaryText := readFileTrimmed(artifacts.SummaryPath)
	facts, _ := memory.ReadBullets(artifacts.FactsPath)
	focusText := readFileTrimmed(artifacts.FocusPath)
	history := r.readChatHistory(artifacts.ChatPath)
	workingDir := r.currentWorkingDir()
	recentLog := r.readRecentObservations(artifacts, 3)
	if recentLog == "" {
		recentLog = r.readRecentLogSnippet(artifacts)
		if snippets := r.readRecentLogSnippets(artifacts, 3); snippets != "" {
			recentLog = snippets
		}
	}
	playbooks := r.playbookHints(goal)
	tools := r.toolsSummary(sessionDir, 12)
	planPath := filepath.Join(sessionDir, r.cfg.Session.PlanFilename)
	inventoryPath := filepath.Join(sessionDir, r.cfg.Session.InventoryFilename)
	targets := append([]string{}, r.cfg.Scope.Targets...)
	if len(targets) == 0 {
		if target := strings.TrimSpace(r.bestKnownTarget()); target != "" {
			targets = append(targets, target)
		}
	}
	return assist.Input{
		SessionID:   r.sessionID,
		Scope:       r.cfg.Scope.Networks,
		Targets:     targets,
		Summary:     summaryText,
		KnownFacts:  facts,
		Focus:       focusText,
		ChatHistory: history,
		WorkingDir:  workingDir,
		RecentLog:   recentLog,
		Playbooks:   playbooks,
		Tools:       tools,
		Mode:        mode,
		Plan:        readFileTrimmed(planPath),
		Inventory:   readFileTrimmed(inventoryPath),
		Goal:        strings.TrimSpace(goal),
	}, nil
}

func (r *Runner) buildAskPrompt(sessionDir, question string) string {
	artifacts, _ := memory.EnsureArtifacts(sessionDir)
	summary := readFileTrimmed(artifacts.SummaryPath)
	facts := readFileTrimmed(artifacts.FactsPath)
	focus := readFileTrimmed(artifacts.FocusPath)
	history := r.readChatHistory(artifacts.ChatPath)
	recentLogs := r.readRecentLogSnippets(artifacts, 3)
	planPath := filepath.Join(sessionDir, r.cfg.Session.PlanFilename)
	inventoryPath := filepath.Join(sessionDir, r.cfg.Session.InventoryFilename)

	builder := strings.Builder{}
	builder.WriteString("User question: " + question + "\n")
	if history != "" {
		builder.WriteString("\nRecent conversation:\n" + history + "\n")
	}
	if recentLogs != "" {
		builder.WriteString("\nRecent log snippets:\n" + recentLogs + "\n")
	}
	if summary != "" {
		builder.WriteString("\nSummary:\n" + summary + "\n")
	}
	if facts != "" {
		builder.WriteString("\nKnown facts:\n" + facts + "\n")
	}
	if focus != "" {
		builder.WriteString("\nFocus:\n" + focus + "\n")
	}
	plan := readFileTrimmed(planPath)
	if plan != "" {
		builder.WriteString("\nPlan:\n" + plan + "\n")
	}
	inventory := readFileTrimmed(inventoryPath)
	if inventory != "" {
		builder.WriteString("\nInventory:\n" + inventory + "\n")
	}
	return builder.String()
}

func (r *Runner) readChatHistory(path string) string {
	if r.cfg.Context.ChatHistoryLines <= 0 || path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	lines = filterNonEmpty(lines)
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > r.cfg.Context.ChatHistoryLines {
		lines = lines[len(lines)-r.cfg.Context.ChatHistoryLines:]
	}
	return strings.Join(lines, "\n")
}

func (r *Runner) appendChatHistory(path, role, content string) {
	if r.cfg.Context.ChatHistoryLines <= 0 || path == "" {
		return
	}
	clean := strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
	if clean == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s: %s\n", role, clean)
}

func filterNonEmpty(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func (r *Runner) currentWorkingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func (r *Runner) readRecentLogSnippet(artifacts memory.Artifacts) string {
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		return ""
	}
	if len(state.RecentLogs) == 0 {
		return ""
	}
	path := state.RecentLogs[len(state.RecentLogs)-1]
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	const maxBytes = 2000
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	return fmt.Sprintf("[log: %s]\n%s", path, content)
}

func (r *Runner) readRecentLogSnippets(artifacts memory.Artifacts, maxLogs int) string {
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		return ""
	}
	if len(state.RecentLogs) == 0 {
		return ""
	}
	paths := state.RecentLogs
	if maxLogs > 0 && len(paths) > maxLogs {
		paths = paths[len(paths)-maxLogs:]
	}
	const maxBytes = 1200
	builder := strings.Builder{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > maxBytes {
			data = data[len(data)-maxBytes:]
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("[log: %s]\n%s\n", path, content))
	}
	return strings.TrimSpace(builder.String())
}

func (r *Runner) readRecentObservations(artifacts memory.Artifacts, max int) string {
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		return ""
	}
	if len(state.RecentObservations) == 0 {
		return ""
	}
	obs := state.RecentObservations
	if max > 0 && len(obs) > max {
		obs = obs[len(obs)-max:]
	}
	builder := strings.Builder{}
	for _, item := range obs {
		cmdLine := strings.TrimSpace(strings.Join(append([]string{item.Command}, item.Args...), " "))
		if cmdLine == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("[%s] %s: %s (exit=%d)", fallbackBlock(item.Time), fallbackBlock(item.Kind), cmdLine, item.ExitCode))
		if strings.TrimSpace(item.LogPath) != "" {
			builder.WriteString(fmt.Sprintf(" log=%s", item.LogPath))
		}
		builder.WriteString("\n")
		if strings.TrimSpace(item.Error) != "" {
			builder.WriteString("error: " + truncate(item.Error, 260) + "\n")
		}
		if strings.TrimSpace(item.OutputExcerpt) != "" {
			builder.WriteString("out: " + truncate(item.OutputExcerpt, 520) + "\n")
		}
	}
	return strings.TrimSpace(builder.String())
}

func (r *Runner) playbookHints(goal string) string {
	if r.cfg.Context.PlaybookMax == 0 {
		return ""
	}
	entries, err := playbook.Load(filepath.Join("docs", "playbooks"))
	if err != nil || len(entries) == 0 {
		return ""
	}
	text := strings.TrimSpace(goal)
	if text == "" {
		return ""
	}
	matches := playbook.Match(entries, text, r.cfg.Context.PlaybookMax)
	if len(matches) == 0 {
		lower := strings.ToLower(text)
		if strings.Contains(lower, "playbook") || strings.Contains(lower, "workflow") || strings.Contains(lower, "procedure") {
			return playbook.List(entries)
		}
		return ""
	}
	return playbook.Render(matches, r.cfg.Context.PlaybookLines)
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	builder := strings.Builder{}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func fallbackBlock(content string) string {
	if strings.TrimSpace(content) == "" {
		return "(empty)"
	}
	return content
}

func (r *Runner) assistGenerator() assist.Assistant {
	assistTemp, assistTokens := r.llmRoleOptions("assist", 0.15, 1200)
	recoveryTemp, recoveryTokens := r.llmRoleOptions("recovery", 0.1, 900)
	llmAssistant := assist.LLMAssistant{
		Client:            llm.NewLMStudioClient(r.cfg),
		Model:             r.cfg.LLM.Model,
		Temperature:       r.float32Ptr(assistTemp),
		MaxTokens:         r.intPtr(assistTokens),
		RepairTemperature: r.float32Ptr(recoveryTemp),
		RepairMaxTokens:   r.intPtr(recoveryTokens),
	}
	fallback := assist.FallbackAssistant{}
	return guardedAssistant{
		allow:     r.llmAllowed,
		onSuccess: r.recordLLMSuccess,
		onFailure: func(err error) {
			if shouldCountLLMFailure(err) {
				r.recordLLMFailure(err)
			}
		},
		onFallback: func(err error) {
			r.logAssistFallbackCause(err)
		},
		primary:  llmAssistant,
		fallback: fallback,
	}
}

func (r *Runner) logAssistFallbackCause(err error) {
	if err == nil {
		return
	}
	if !r.cfg.UI.Verbose {
		r.logger.Printf("LLM response unusable; fallback assistant used. Run /verbose on for details.")
		return
	}
	r.logger.Printf("LLM fallback reason: %v", err)
	var parseErr assist.SuggestionParseError
	if errors.As(err, &parseErr) {
		raw := collapseWhitespace(strings.TrimSpace(parseErr.Raw))
		if raw != "" {
			r.logger.Printf("LLM raw response: %s", truncate(raw, 500))
		}
	}
}

func shouldCountLLMFailure(err error) bool {
	if err == nil {
		return false
	}
	// Parse/format errors mean we reached the model but got unusable output.
	// Do not trip cooldown for transport-level guardrails on these.
	var parseErr assist.SuggestionParseError
	return !errors.As(err, &parseErr)
}

func (r *Runner) getAssistSuggestion(goal string, mode string) (assist.Suggestion, error) {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return assist.Suggestion{}, err
	}
	input, err := r.assistInput(sessionDir, r.enrichAssistGoal(goal, mode), mode)
	if err != nil {
		return assist.Suggestion{}, err
	}
	assistant := r.assistGenerator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return assistant.Suggest(ctx, input)
}
