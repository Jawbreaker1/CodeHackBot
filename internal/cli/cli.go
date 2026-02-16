package cli

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	neturl "net/url"
	"os"
	goexec "os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
	"github.com/Jawbreaker1/CodeHackBot/internal/playbook"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func (r *Runner) handleAssist(args []string) error {
	r.setTask("assist")
	defer r.clearTask()

	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: assist not permitted")
	}
	dryRun := false
	goal := ""
	if len(args) > 0 && strings.ToLower(args[0]) == "dry" {
		dryRun = true
		goal = strings.Join(args[1:], " ")
	} else {
		goal = strings.Join(args, " ")
	}
	r.appendConversation("User", goal)
	return r.handleAssistGoal(goal, dryRun)
}

// Moved: Runner + main loop live in runner.go; command dispatcher lives in commands.go; session commands live in cmd_session.go; plan commands live in cmd_plan.go; exec commands live in cmd_exec.go; misc tool commands live in cmd_tools.go.

func (r *Runner) handleAsk(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if !r.llmAllowed() {
		r.logger.Printf("LLM unavailable; configure llm.base_url or wait for cooldown.")
		return nil
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	prompt := r.buildAskPrompt(sessionDir, text)
	r.appendConversation("User", text)
	client := llm.NewLMStudioClient(r.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("ask")
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: 0.2,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: "You are BirdHackBot, a security testing assistant operating in an authorized lab owned by the user. Never claim to be Claude, OpenAI, Anthropic, or any other assistant identity. Provide direct help within scope, including inspecting local files or assisting with encrypted files the user created. If access requires a password, ask for it; if asked to crack a password, proceed only when the user confirms authorization for that file. Do not emit tool-call markup; respond in plain text. If web access is needed, ask the user to run /browse with the URL (it requires approval). Answer clearly and concisely and ask follow-up questions when needed.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
	})
	stopIndicator()
	if err != nil {
		r.recordLLMFailure(err)
		return err
	}
	r.recordLLMSuccess()
	if r.cfg.UI.Verbose {
		r.logger.Printf("Assistant response:")
	}
	msg := normalizeAssistantOutput(resp.Content)
	fmt.Println(msg)
	r.appendConversation("Assistant", msg)
	return nil
}

func (r *Runner) memoryManager(sessionDir string) memory.Manager {
	return memory.Manager{
		SessionDir:         sessionDir,
		LogDir:             filepath.Join(sessionDir, "logs"),
		PlanFilename:       r.cfg.Session.PlanFilename,
		LedgerFilename:     r.cfg.Session.LedgerFilename,
		LedgerEnabled:      r.cfg.Session.LedgerEnabled,
		MaxRecentOutputs:   r.cfg.Context.MaxRecentOutputs,
		SummarizeEvery:     r.cfg.Context.SummarizeEvery,
		SummarizeAtPercent: r.cfg.Context.SummarizeAtPercent,
		ChatHistoryLines:   r.cfg.Context.ChatHistoryLines,
	}
}

func (r *Runner) summaryGenerator() memory.Summarizer {
	primary := memory.LLMSummarizer{Client: llm.NewLMStudioClient(r.cfg), Model: r.cfg.LLM.Model}
	fallback := memory.FallbackSummarizer{}
	return guardedSummarizer{
		allow:     r.llmAllowed,
		onSuccess: r.recordLLMSuccess,
		onFailure: r.recordLLMFailure,
		primary:   primary,
		fallback:  fallback,
	}
}

func (r *Runner) maybeAutoSummarize(logPath, reason string) {
	if logPath == "" {
		return
	}
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	manager := r.memoryManager(sessionDir)
	state, err := manager.RecordLog(logPath)
	if err != nil {
		r.logger.Printf("Context tracking failed: %v", err)
		return
	}
	if !manager.ShouldSummarize(state) {
		return
	}
	if r.cfg.Permissions.Level == "readonly" {
		r.logger.Printf("Auto-summarize skipped (readonly)")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("summarize")
	defer stopIndicator()
	if err := manager.Summarize(ctx, r.summaryGenerator(), reason); err != nil {
		r.logger.Printf("Auto-summarize failed: %v", err)
		return
	}
	r.logger.Printf("Auto-summary updated")
}

func (r *Runner) llmAvailable() bool {
	baseURL := strings.TrimSpace(r.cfg.LLM.BaseURL)
	if baseURL != "" {
		return true
	}
	return !r.cfg.Network.AssumeOffline
}

func (r *Runner) llmAllowed() bool {
	if !r.llmAvailable() {
		return false
	}
	return r.llmGuard.Allow()
}

func (r *Runner) recordLLMFailure(err error) {
	if err == nil {
		return
	}
	r.llmGuard.RecordFailure()
	if !r.llmGuard.Allow() {
		until := r.llmGuard.DisabledUntil()
		if !until.IsZero() {
			r.logger.Printf("LLM disabled until %s after %d failures.", until.Format(time.RFC3339), r.llmGuard.Failures())
		}
	}
}

func (r *Runner) recordLLMSuccess() {
	r.llmGuard.RecordSuccess()
}

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
	llmAssistant := assist.LLMAssistant{Client: llm.NewLMStudioClient(r.cfg), Model: r.cfg.LLM.Model}
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

func (r *Runner) handleAssistGoal(goal string, dryRun bool) error {
	trimmedGoal := strings.TrimSpace(goal)
	if trimmedGoal != "" {
		r.updateKnownTargetFromText(trimmedGoal)
		r.updateTaskFoundation(trimmedGoal)
		r.pendingAssistGoal = ""
		r.pendingAssistQ = ""
		r.resetAssistLoopState()
	}
	if !dryRun && isSummaryIntent(trimmedGoal) && strings.TrimSpace(r.lastActionLogPath) != "" {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Using latest artifact for summary: %s", r.lastActionLogPath)
		}
		return r.summarizeFromLatestArtifact(trimmedGoal)
	}
	return r.handleAssistGoalWithMode(goal, dryRun, "")
}

func (r *Runner) handleAssistGoalWithMode(goal string, dryRun bool, mode string) error {
	if r.cfg.Permissions.Level == "readonly" {
		return fmt.Errorf("readonly mode: assist not permitted")
	}
	if !r.llmAllowed() && r.cfg.UI.Verbose {
		switch {
		case !r.llmAvailable():
			r.logger.Printf("LLM unavailable; check llm.base_url or network config.")
		case !r.llmGuard.DisabledUntil().IsZero():
			r.logger.Printf("LLM cooldown active until %s; using fallback assistant.", r.llmGuard.DisabledUntil().Format(time.RFC3339))
		default:
			r.logger.Printf("LLM unavailable; using fallback assistant.")
		}
	}
	if mode == "execute-step" {
		return r.handleAssistSingleStep(goal, dryRun, mode)
	}
	return r.handleAssistAgentic(goal, dryRun, mode)
}

func (r *Runner) handleAssistSingleStep(goal string, dryRun bool, mode string) error {
	stopIndicator := r.startLLMIndicatorIfAllowed("assist")
	suggestion, err := r.getAssistSuggestion(goal, mode)
	stopIndicator()
	if err != nil {
		return err
	}
	if suggestion.Type == "noop" && strings.TrimSpace(goal) != "" {
		return r.handleAssistNoop(goal, dryRun)
	}
	if err := r.executeAssistSuggestion(suggestion, dryRun); err != nil {
		if r.handleAssistCommandFailure(goal, suggestion, err) {
			r.maybeEmitGoalSummary(goal, dryRun)
			return nil
		}
		return err
	}
	if !dryRun && suggestion.Type == "command" {
		r.pendingAssistGoal = ""
		r.maybeSuggestNextSteps(goal, suggestion)
	}
	return nil
}

func (r *Runner) handleAssistAgentic(goal string, dryRun bool, mode string) error {
	if mode != "web-agentic" {
		if url := extractFirstURL(goal); url != "" && shouldAutoBrowse(goal) {
			return r.handleAssistAgentic(fmt.Sprintf("%s (fetch and analyze this URL, then summarize)", goal), dryRun, "web-agentic")
		}
	}

	budget := newAssistBudget(goal, r.assistMaxSteps())
	r.startAssistRuntime(goal, mode, budget)
	defer r.clearAssistRuntime()
	repeatedGuardHits := 0
	stepMode := mode
	lastCommand := assist.Suggestion{}
	for {
		r.updateAssistRuntime(stepMode, budget)
		if budget.exhausted() {
			if r.cfg.UI.Verbose {
				r.logger.Printf("Reached step budget (%d/%d).", budget.used, budget.currentCap)
			}
			if !dryRun {
				if r.tryConcludeGoalFromArtifacts(goal) {
					r.maybeFinalizeReport(goal, dryRun)
					return nil
				}
				fmt.Println("Reached dynamic step budget without a final answer. Suggesting next steps.")
			}
			if !dryRun && lastCommand.Type == "command" {
				r.maybeSuggestNextSteps(goal, lastCommand)
			}
			r.maybeEmitGoalSummary(goal, dryRun)
			r.maybeFinalizeReport(goal, dryRun)
			return nil
		}
		stepNum, maxSteps := budget.stepLabel()
		if r.cfg.UI.Verbose {
			if maxSteps > 0 {
				r.logger.Printf("Assistant step %d/%d", stepNum, maxSteps)
			} else {
				r.logger.Printf("Assistant step %d", stepNum)
			}
		}
		label := "assist"
		if maxSteps > 0 {
			label = fmt.Sprintf("assist %d/%d", stepNum, maxSteps)
		}
		stopIndicator := r.startLLMIndicatorIfAllowed(label)
		suggestion, err := r.getAssistSuggestion(goal, stepMode)
		stopIndicator()
		if err != nil {
			return err
		}
		if suggestion.Type == "noop" && strings.TrimSpace(goal) != "" {
			budget.consume("noop -> clarify")
			budget.onStall("noop suggestion")
			r.updateAssistRuntime("recover", budget)
			return r.handleAssistNoop(goal, dryRun)
		}
		r.announceAssistStep(stepNum, maxSteps, suggestion)
		if suggestion.Type == "plan" {
			budget.consume("plan returned")
			budget.onProgress("assistant returned executable plan")
			r.updateAssistRuntime(stepMode, budget)
			if err := r.handlePlanSuggestion(suggestion, dryRun); err != nil {
				r.maybeFinalizeReport(goal, dryRun)
				return err
			}
			r.maybeEmitGoalSummary(goal, dryRun)
			r.maybeFinalizeReport(goal, dryRun)
			return nil
		}
		if suggestion.Type == "complete" {
			budget.consume("goal completed")
			budget.onProgress("assistant completion")
			r.updateAssistRuntime(stepMode, budget)
			_ = r.executeAssistSuggestion(suggestion, dryRun)
			r.maybeFinalizeReport(goal, dryRun)
			return nil
		}
		beforeObs := r.latestObservationSignature()
		actionKey := ""
		if suggestion.Type == "command" {
			actionKey = canonicalAssistActionKey(suggestion.Command, suggestion.Args)
		}
		if err := r.executeAssistSuggestion(suggestion, dryRun); err != nil {
			if isAssistRepeatedGuard(err) {
				if !dryRun && r.tryConcludeGoalFromArtifacts(goal) {
					r.maybeFinalizeReport(goal, dryRun)
					return nil
				}
				repeatedGuardHits++
				budget.onStall("repeated step blocked")
				r.updateAssistRuntime("recover", budget)
				if repeatedGuardHits >= 2 {
					msg := "Repeated step loop detected. Pausing for guidance: share the exact next action/target and I will continue."
					fmt.Println(msg)
					r.appendConversation("Assistant", msg)
					r.pendingAssistGoal = goal
					r.pendingAssistQ = msg
					return nil
				}
				if !dryRun {
					if r.cfg.UI.Verbose {
						r.logger.Printf("Repeated step detected; requesting an alternative action.")
					} else {
						fmt.Println("Repeated step detected; asking assistant for an alternative action.")
					}
				}
				stepMode = "recover"
				continue
			}
			budget.consume("step failed")
			budget.onStall("step execution failed")
			r.updateAssistRuntime("recover", budget)
			if r.handleAssistCommandFailure(goal, suggestion, err) {
				r.maybeEmitGoalSummary(goal, dryRun)
				r.maybeFinalizeReport(goal, dryRun)
				return nil
			}
			r.maybeFinalizeReport(goal, dryRun)
			return err
		}
		if dryRun {
			return nil
		}
		repeatedGuardHits = 0
		budget.consume("step executed")
		switch suggestion.Type {
		case "question":
			budget.onProgress("awaiting user input")
		case "command", "tool":
			afterObs := r.latestObservationSignature()
			if progressed, reason := budget.trackProgress(actionKey, beforeObs, afterObs); progressed {
				budget.onProgress(reason)
			} else {
				budget.onStall(reason)
			}
		default:
			budget.onStall("no measurable progress")
		}
		r.updateAssistRuntime(stepMode, budget)
		if (suggestion.Type == "command" || suggestion.Type == "tool") && r.tryConcludeGoalFromArtifacts(goal) {
			r.maybeFinalizeReport(goal, dryRun)
			return nil
		}
		if suggestion.Type == "question" {
			r.pendingAssistGoal = goal
			return nil
		}
		if suggestion.Type == "complete" {
			r.pendingAssistGoal = ""
			r.pendingAssistQ = ""
			return nil
		}
		if suggestion.Type == "command" {
			lastCommand = suggestion
			r.pendingAssistGoal = ""
			r.pendingAssistQ = ""
		}
		stepMode = "execute-step"
	}
}

func (r *Runner) announceAssistStep(stepNum, maxSteps int, suggestion assist.Suggestion) {
	if r.cfg.UI.Verbose || suggestion.Type == "noop" {
		return
	}
	desc := assistStepDescription(suggestion)
	if desc == "" {
		return
	}
	if maxSteps > 0 {
		fmt.Printf("Step %d/%d: %s\n", stepNum, maxSteps, desc)
		return
	}
	fmt.Printf("Step %d: %s\n", stepNum, desc)
}

func assistStepDescription(suggestion assist.Suggestion) string {
	if text := collapseWhitespace(strings.TrimSpace(suggestion.Summary)); text != "" {
		return truncate(text, 140)
	}
	switch suggestion.Type {
	case "command":
		cmdLine := strings.TrimSpace(strings.Join(append([]string{suggestion.Command}, suggestion.Args...), " "))
		if cmdLine == "" {
			return "running command"
		}
		return "running: " + truncate(cmdLine, 140)
	case "tool":
		if suggestion.Tool == nil {
			return "building helper tool"
		}
		parts := []string{"building tool"}
		if name := strings.TrimSpace(suggestion.Tool.Name); name != "" {
			parts = append(parts, name)
		}
		if purpose := collapseWhitespace(strings.TrimSpace(suggestion.Tool.Purpose)); purpose != "" {
			parts = append(parts, "for "+truncate(purpose, 90))
		}
		return strings.Join(parts, " ")
	case "question":
		if q := collapseWhitespace(strings.TrimSpace(suggestion.Question)); q != "" {
			return "needs input: " + truncate(q, 120)
		}
		return "needs user input"
	case "plan":
		if len(suggestion.Steps) > 0 {
			return fmt.Sprintf("proposed plan with %d step(s)", len(suggestion.Steps))
		}
		return "proposed plan"
	case "complete":
		return "goal completed"
	default:
		return ""
	}
}

func (r *Runner) handleAssistNoop(goal string, dryRun bool) error {
	clarifyGoal := fmt.Sprintf("Original goal: %s\nThe previous suggestion was noop. Provide one concrete next step. If details are missing, ask one concise clarifying question.", goal)
	stopIndicator := r.startLLMIndicatorIfAllowed("assist clarify")
	suggestion, err := r.getAssistSuggestion(clarifyGoal, "recover")
	stopIndicator()
	if err != nil {
		r.pendingAssistGoal = goal
		fmt.Println("I need one more detail to continue. Share what target/path/url to act on.")
		return nil
	}
	if suggestion.Type == "noop" {
		r.pendingAssistGoal = goal
		fmt.Println("I need one more detail to continue. Share what target/path/url to act on.")
		return nil
	}
	if err := r.executeAssistSuggestion(suggestion, dryRun); err != nil {
		if r.handleAssistCommandFailure(goal, suggestion, err) {
			return nil
		}
		return err
	}
	if suggestion.Type == "question" {
		r.pendingAssistGoal = goal
	} else if suggestion.Type == "command" {
		r.pendingAssistGoal = ""
		r.pendingAssistQ = ""
	}
	return nil
}

func (r *Runner) handleAssistFollowUp(answer string) error {
	goal := strings.TrimSpace(r.pendingAssistGoal)
	prevQuestion := strings.TrimSpace(r.pendingAssistQ)
	r.pendingAssistGoal = ""
	r.pendingAssistQ = ""
	if goal == "" {
		return nil
	}
	answer = strings.TrimSpace(answer)
	if answer != "" {
		r.updateKnownTargetFromText(answer)
	}
	if shouldStartBaselineScan(answer) && strings.TrimSpace(r.lastKnownTarget) != "" {
		target := r.bestKnownTarget()
		if target != "" {
			r.logger.Printf("Using remembered target for baseline non-intrusive scan: %s", target)
			return r.handleRun([]string{"nmap", "-sV", "-Pn", "--top-ports", "100", target})
		}
	}
	combined := fmt.Sprintf("Original goal: %s\nUser answer to previous assistant question: %s\nContinue the task using available tools.", goal, answer)
	if prevQuestion != "" {
		combined += fmt.Sprintf("\nAssistant previous question: %s", prevQuestion)
		combined += "\nDo not repeat the same clarifying question if the answer already resolves it; pick a concrete next action."
	}
	if shouldUseDefaultChoice(answer, prevQuestion) {
		combined += "\nUser explicitly chose the default option. Select a safe default available in the current environment and proceed."
	}
	if target := strings.TrimSpace(r.lastKnownTarget); target != "" {
		combined += fmt.Sprintf("\nCurrent remembered target: %s", target)
	}
	return r.handleAssistGoalWithMode(combined, false, "follow-up")
}

func (r *Runner) assistMaxSteps() int {
	if r.cfg.Agent.MaxSteps > 0 {
		return r.cfg.Agent.MaxSteps
	}
	return 6
}

func (r *Runner) executeAssistSuggestion(suggestion assist.Suggestion, dryRun bool) error {
	if isPlaceholderCommand(suggestion.Command) {
		return fmt.Errorf("assistant returned placeholder command: %s", suggestion.Command)
	}
	switch suggestion.Type {
	case "question":
		if suggestion.Question == "" {
			return fmt.Errorf("assistant returned empty question")
		}
		if err := r.guardAssistQuestionLoop(suggestion.Question); err != nil {
			return err
		}
		if r.cfg.UI.Verbose {
			r.logger.Printf("Assistant question: %s", suggestion.Question)
			if suggestion.Summary != "" {
				r.logger.Printf("Summary: %s", suggestion.Summary)
			}
		} else {
			fmt.Println(normalizeAssistantOutput(suggestion.Question))
		}
		r.appendConversation("Assistant", normalizeAssistantOutput(suggestion.Question))
		r.pendingAssistQ = suggestion.Question
		return nil
	case "noop":
		if r.cfg.UI.Verbose {
			r.logger.Printf("Assistant has no suggestion")
		}
		return nil
	case "complete":
		final := strings.TrimSpace(suggestion.Final)
		if final == "" {
			final = strings.TrimSpace(suggestion.Summary)
		}
		if final == "" {
			final = "(completed)"
		}
		final = normalizeAssistantOutput(final)
		fmt.Println(final)
		r.appendConversation("Assistant", final)
		r.pendingAssistGoal = ""
		r.pendingAssistQ = ""
		return nil
	case "tool":
		if suggestion.Tool == nil {
			return fmt.Errorf("assistant returned tool without tool spec")
		}
		return r.executeToolSuggestion(*suggestion.Tool, dryRun)
	case "plan":
		r.resetAssistLoopState()
		return r.handlePlanSuggestion(suggestion, dryRun)
	case "command":
		if suggestion.Command == "" {
			return fmt.Errorf("assistant returned empty command")
		}
		suggestion.Args = normalizeShellScriptArgs(suggestion.Command, suggestion.Args)
		if script, ok := extractShellScript(suggestion.Command, suggestion.Args); ok && looksLikeFragileShellPipeline(script) {
			return fmt.Errorf("fragile shell pipeline blocked: prefer internal tools (/crawl, /browse, /parse_links, /read_file, /list_dir) or return type=tool to build a helper instead of bash pipelines")
		}
	default:
		return fmt.Errorf("assistant returned unknown type: %s", suggestion.Type)
	}

	r.logger.Printf("Suggested command: %s %s", suggestion.Command, strings.Join(suggestion.Args, " "))
	r.appendConversation("Assistant", fmt.Sprintf("Suggested command: %s %s", suggestion.Command, strings.Join(suggestion.Args, " ")))
	if r.cfg.UI.Verbose {
		if suggestion.Summary != "" {
			r.logger.Printf("Summary: %s", suggestion.Summary)
		}
		if suggestion.Risk != "" {
			r.logger.Printf("Risk: %s", suggestion.Risk)
		}
	}
	if dryRun {
		return nil
	}
	if err := r.guardAssistCommandLoop(suggestion.Command, suggestion.Args); err != nil {
		return err
	}
	r.pendingAssistQ = ""
	if strings.EqualFold(suggestion.Command, "browse") {
		args, err := sanitizeBrowseArgs(suggestion.Args)
		if err != nil {
			return err
		}
		return r.handleBrowse(args)
	}
	if strings.EqualFold(suggestion.Command, "crawl") {
		return r.handleCrawl(suggestion.Args)
	}
	if strings.EqualFold(suggestion.Command, "parse_links") || strings.EqualFold(suggestion.Command, "links") {
		return r.handleParseLinks(suggestion.Args)
	}
	if strings.EqualFold(suggestion.Command, "read_file") || strings.EqualFold(suggestion.Command, "read") {
		return r.handleReadFile(suggestion.Args)
	}
	if strings.EqualFold(suggestion.Command, "list_dir") || strings.EqualFold(suggestion.Command, "ls") {
		return r.handleListDir(suggestion.Args)
	}
	if strings.EqualFold(suggestion.Command, "write_file") || strings.EqualFold(suggestion.Command, "write") {
		return r.handleWriteFile(suggestion.Args)
	}
	if strings.HasPrefix(strings.ToLower(suggestion.Command), "http") && len(suggestion.Args) == 0 {
		return r.handleBrowse([]string{suggestion.Command})
	}
	args := append([]string{suggestion.Command}, suggestion.Args...)
	err := r.handleRun(args)
	if isBenignNoMatchError(err) {
		r.logger.Printf("No matches found for this step; continuing.")
		return nil
	}
	return err
}

func (r *Runner) handlePlanSuggestion(suggestion assist.Suggestion, dryRun bool) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	planText := strings.TrimSpace(suggestion.Plan)
	if planText == "" && len(suggestion.Steps) > 0 {
		builder := strings.Builder{}
		builder.WriteString("## Plan (Assistant)\n\n### Steps\n")
		for i, step := range suggestion.Steps {
			builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
		}
		planText = builder.String()
	}
	if planText != "" {
		planPath, err := session.AppendPlan(sessionDir, r.cfg.Session.PlanFilename, planText)
		if err != nil {
			return err
		}
		if r.cfg.UI.Verbose {
			r.logger.Printf("Plan updated: %s", planPath)
		}
	}
	if planText != "" {
		fmt.Println("Plan:")
		fmt.Println(planText)
		r.appendConversation("Assistant", planText)
	}
	if len(suggestion.Steps) > 0 {
		fmt.Println("Plan steps:")
		steps := suggestion.Steps
		maxSteps := r.assistMaxSteps()
		if maxSteps > 0 && len(steps) > maxSteps {
			steps = steps[:maxSteps]
		}
		for i, step := range steps {
			fmt.Printf("%d) %s\n", i+1, step)
		}
		r.appendConversation("Assistant", "Plan steps: "+strings.Join(steps, " | "))
		suggestion.Steps = steps
	}
	if dryRun {
		return nil
	}
	if len(suggestion.Steps) == 0 {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Plan has no executable steps.")
		}
		return nil
	}
	for i, step := range suggestion.Steps {
		r.logger.Printf("Executing step %d/%d: %s", i+1, len(suggestion.Steps), step)
		stopIndicator := r.startLLMIndicatorIfAllowed("assist")
		result, err := r.getAssistSuggestion(step, "execute-step")
		stopIndicator()
		if err != nil {
			return err
		}
		if err := r.executeAssistSuggestion(result, false); err != nil {
			if r.handleAssistCommandFailure(step, result, err) {
				return nil
			}
			return err
		}
		if result.Type == "question" {
			r.logger.Printf("Plan paused for user input. Continue after answering.")
			break
		}
	}
	return nil
}

func (r *Runner) handleAssistCommandFailure(goal string, suggestion assist.Suggestion, err error) bool {
	if err == nil {
		return false
	}
	// If the user denied an approval prompt, don't try to "recover" automatically.
	lowerErr := strings.ToLower(err.Error())
	if strings.Contains(lowerErr, "not approved") || strings.Contains(lowerErr, "fetch not approved") || strings.Contains(lowerErr, "network access not approved") {
		r.logger.Printf("Aborted by user.")
		return true
	}
	var cmdErr commandError
	if !errors.As(err, &cmdErr) {
		cmdErr = commandError{
			Result: exec.CommandResult{
				Command: suggestion.Command,
				Args:    suggestion.Args,
			},
			Err: err,
		}
	}
	summary := summarizeCommandFailure(cmdErr)
	if summary != "" {
		r.logger.Printf("Command failed: %s", summary)
	} else {
		r.logger.Printf("Command failed: %v", cmdErr.Err)
	}
	if hint := assistFailureHint(suggestion, cmdErr); hint != "" {
		r.logger.Printf("Hint: %s", hint)
	}
	if r.tryAssistRecovery(suggestion, cmdErr) {
		return true
	}
	return r.suggestAssistRecovery(goal, suggestion, cmdErr)
}

func summarizeCommandFailure(cmdErr commandError) string {
	parts := []string{}
	if cmdErr.Err != nil {
		parts = append(parts, cmdErr.Err.Error())
	}
	if output := firstLines(cmdErr.Result.Output, 2); output != "" {
		parts = append(parts, output)
	}
	return strings.Join(parts, " | ")
}

func (r *Runner) suggestAssistRecovery(goal string, suggestion assist.Suggestion, cmdErr commandError) bool {
	if !r.llmAllowed() {
		r.logger.Printf("No recovery suggestion (LLM unavailable).")
		return true
	}
	recoveryGoal := buildRecoveryGoal(goal, suggestion, cmdErr)
	label := "recover"
	stopIndicator := r.startLLMIndicatorIfAllowed(label)
	recovery, err := r.getAssistSuggestion(recoveryGoal, "recover")
	stopIndicator()
	if err != nil {
		r.logger.Printf("Recovery suggestion failed: %v", err)
		return true
	}
	if recovery.Type == "noop" {
		r.logger.Printf("No recovery suggestion provided.")
		return true
	}
	if recovery.Type == "question" {
		fmt.Println(recovery.Question)
		r.appendConversation("Assistant", recovery.Question)
		if strings.TrimSpace(goal) != "" {
			r.pendingAssistGoal = goal
		}
		return true
	}
	if recovery.Type == "plan" {
		_ = r.handlePlanSuggestion(recovery, true)
		return true
	}
	if recovery.Type == "tool" && recovery.Tool != nil {
		r.logger.Printf("Recovery suggestion: tool %s (%s)", fallbackBlock(recovery.Tool.Name), fallbackBlock(recovery.Tool.Language))
		if recovery.Tool.Purpose != "" {
			r.logger.Printf("Recovery purpose: %s", recovery.Tool.Purpose)
		}
		if err := r.executeToolSuggestion(*recovery.Tool, false); err != nil {
			r.logger.Printf("Recovery attempt failed: %v", err)
		}
		return true
	}
	if recovery.Type == "command" {
		r.logger.Printf("Recovery suggestion: %s %s", recovery.Command, strings.Join(recovery.Args, " "))
		if recovery.Summary != "" {
			r.logger.Printf("Recovery summary: %s", recovery.Summary)
		}
		if recovery.Risk != "" {
			r.logger.Printf("Recovery risk: %s", recovery.Risk)
		}
		if err := r.executeAssistSuggestion(recovery, false); err != nil {
			r.logger.Printf("Recovery attempt failed: %v", err)
		}
		return true
	}
	r.logger.Printf("Recovery suggestion returned unknown type: %s", recovery.Type)
	return true
}

func (r *Runner) maybeSuggestNextSteps(goal string, lastSuggestion assist.Suggestion) {
	if !r.llmAllowed() {
		return
	}
	if strings.TrimSpace(goal) == "" && lastSuggestion.Summary == "" {
		return
	}
	nextGoal := buildNextStepsGoal(goal, lastSuggestion)
	stopIndicator := r.startLLMIndicatorIfAllowed("next steps")
	next, err := r.getAssistSuggestion(nextGoal, "next-steps")
	stopIndicator()
	if err != nil {
		r.logger.Printf("Next-step suggestion failed: %v", err)
		return
	}
	switch next.Type {
	case "question":
		if next.Question != "" {
			fmt.Println(next.Question)
			r.appendConversation("Assistant", next.Question)
			if strings.TrimSpace(goal) != "" {
				r.pendingAssistGoal = goal
			}
		}
	case "plan":
		steps := next.Steps
		if len(steps) == 0 && next.Plan != "" {
			fmt.Println("Possible next steps:")
			fmt.Println(next.Plan)
			r.appendConversation("Assistant", "Possible next steps: "+next.Plan)
			return
		}
		if len(steps) > 0 {
			fmt.Println("Possible next steps:")
			for i, step := range steps {
				fmt.Printf("%d) %s\n", i+1, step)
			}
			r.appendConversation("Assistant", "Possible next steps: "+strings.Join(steps, " | "))
		}
	case "command":
		cmdLine := strings.TrimSpace(strings.Join(append([]string{next.Command}, next.Args...), " "))
		if cmdLine != "" {
			fmt.Printf("Suggested next command: %s\n", cmdLine)
		}
		if next.Summary != "" {
			fmt.Printf("Why: %s\n", next.Summary)
		}
		r.appendConversation("Assistant", fmt.Sprintf("Suggested next command: %s", cmdLine))
	default:
		if r.cfg.UI.Verbose {
			r.logger.Printf("No next-step suggestion.")
		}
	}
}

func (r *Runner) maybeEmitGoalSummary(goal string, dryRun bool) {
	if dryRun {
		return
	}
	goal = strings.TrimSpace(goal)
	if !isSummaryIntent(goal) {
		return
	}
	if strings.TrimSpace(r.lastActionLogPath) == "" {
		return
	}
	if err := r.summarizeFromLatestArtifact(goal); err != nil && r.cfg.UI.Verbose {
		r.logger.Printf("Summary generation failed: %v", err)
	}
}

func (r *Runner) summarizeFromLatestArtifact(goal string) error {
	if strings.TrimSpace(goal) == "" || strings.TrimSpace(r.lastActionLogPath) == "" {
		return nil
	}
	if !r.llmAllowed() {
		return nil
	}
	artifactPath := r.lastActionLogPath
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return fmt.Errorf("read latest artifact: %w", err)
	}
	const maxArtifactBytes = 16000
	if len(data) > maxArtifactBytes {
		data = data[len(data)-maxArtifactBytes:]
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	artifacts, _ := memory.EnsureArtifacts(sessionDir)
	contextSummary := readFileTrimmed(artifacts.SummaryPath)
	contextFacts := readFileTrimmed(artifacts.FactsPath)
	contextFocus := readFileTrimmed(artifacts.FocusPath)
	prompt := strings.Builder{}
	prompt.WriteString("Goal:\n")
	prompt.WriteString(goal + "\n\n")
	prompt.WriteString("Latest action artifact path:\n")
	prompt.WriteString(artifactPath + "\n\n")
	prompt.WriteString("Latest action artifact content:\n")
	prompt.WriteString(string(data) + "\n\n")
	if contextSummary != "" {
		prompt.WriteString("Session summary:\n" + contextSummary + "\n\n")
	}
	if contextFacts != "" {
		prompt.WriteString("Known facts:\n" + contextFacts + "\n\n")
	}
	if contextFocus != "" {
		prompt.WriteString("Task foundation:\n" + contextFocus + "\n\n")
	}
	prompt.WriteString("Provide: 1) concise summary, 2) known findings, 3) unknown/missing data, 4) next 2-3 concrete steps.")

	client := llm.NewLMStudioClient(r.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("summary")
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: 0.2,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: "You are BirdHackBot. Summarize from provided artifact/content only. Do not ask user to paste files if artifact content is already present.",
			},
			{
				Role:    "user",
				Content: prompt.String(),
			},
		},
	})
	stopIndicator()
	if err != nil {
		r.recordLLMFailure(err)
		return err
	}
	r.recordLLMSuccess()
	text := normalizeAssistantOutput(resp.Content)
	fmt.Println(text)
	r.appendConversation("Assistant", text)
	return nil
}

func assistFailureHint(suggestion assist.Suggestion, cmdErr commandError) string {
	command := strings.ToLower(strings.TrimSpace(suggestion.Command))
	outputLower := strings.ToLower(cmdErr.Result.Output)
	errLower := ""
	if cmdErr.Err != nil {
		errLower = strings.ToLower(cmdErr.Err.Error())
	}
	if command == "whois" {
		if strings.Contains(outputLower, "no match") || strings.Contains(outputLower, "not found") {
			return "WHOIS typically only supports root domains (e.g., systemverification.com), not subdomains."
		}
	}
	if command == "fcrackzip" {
		if strings.Contains(outputLower, "rockyou.txt: no such file or directory") || strings.Contains(errLower, "rockyou.txt: no such file or directory") {
			return "Kali often ships rockyou as /usr/share/wordlists/rockyou.txt.gz. Extract it or use that gz source to create a local wordlist file."
		}
	}
	if strings.Contains(errLower, "executable file not found") || strings.Contains(errLower, "not found") {
		return "Tool not available in PATH. Install it or update your tool inventory."
	}
	return ""
}

func (r *Runner) tryAssistRecovery(suggestion assist.Suggestion, cmdErr commandError) bool {
	command := strings.ToLower(strings.TrimSpace(suggestion.Command))
	switch command {
	case "whois":
		return r.tryWhoisRecovery(cmdErr)
	case "fcrackzip":
		return r.tryFcrackzipWordlistRecovery(cmdErr)
	default:
		return false
	}
}

func (r *Runner) tryWhoisRecovery(cmdErr commandError) bool {
	if len(cmdErr.Result.Args) == 0 {
		return false
	}
	original := cmdErr.Result.Args[0]
	alt, ok := normalizeWhoisTarget(original)
	if !ok || strings.EqualFold(alt, original) {
		return false
	}
	outputLower := strings.ToLower(cmdErr.Result.Output)
	if outputLower != "" && !strings.Contains(outputLower, "no match") && !strings.Contains(outputLower, "not found") {
		return false
	}
	r.logger.Printf("Retrying whois with root domain: %s", alt)
	if retryErr := r.handleRun([]string{"whois", alt}); retryErr != nil {
		r.logger.Printf("Retry failed: %v", retryErr)
	}
	return true
}

func (r *Runner) tryFcrackzipWordlistRecovery(cmdErr commandError) bool {
	args := append([]string{}, cmdErr.Result.Args...)
	if len(args) == 0 {
		return false
	}
	wordlistIdx, wordlistArg, ok := fcrackzipWordlistArg(args)
	if !ok {
		return false
	}
	if _, err := os.Stat(wordlistArg); err == nil {
		return false
	}
	combined := strings.ToLower(cmdErr.Result.Output)
	if cmdErr.Err != nil {
		combined += "\n" + strings.ToLower(cmdErr.Err.Error())
	}
	if !strings.Contains(combined, "no such file or directory") {
		return false
	}
	gzSource := strings.TrimSpace(wordlistArg + ".gz")
	if _, err := os.Stat(gzSource); err != nil {
		// Common Kali location fallback.
		gzSource = "/usr/share/wordlists/rockyou.txt.gz"
		if _, err := os.Stat(gzSource); err != nil {
			return false
		}
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		r.logger.Printf("Recovery setup failed: %v", err)
		return false
	}
	outDir := filepath.Join(sessionDir, "artifacts", "wordlists")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		r.logger.Printf("Recovery setup failed: %v", err)
		return false
	}
	extracted := filepath.Join(outDir, "rockyou.txt")
	if _, statErr := os.Stat(extracted); statErr != nil {
		if extractErr := extractGzipFile(gzSource, extracted); extractErr != nil {
			r.logger.Printf("Recovery extraction failed: %v", extractErr)
			return false
		}
	}
	if strings.HasPrefix(args[wordlistIdx], "-p") && args[wordlistIdx] != "-p" {
		args[wordlistIdx] = "-p" + extracted
	} else {
		args[wordlistIdx] = extracted
	}
	r.logger.Printf("Retrying fcrackzip with recovered wordlist: %s", extracted)
	if retryErr := r.handleRun(append([]string{"fcrackzip"}, args...)); retryErr != nil {
		r.logger.Printf("Retry failed: %v", retryErr)
	}
	return true
}

func fcrackzipWordlistArg(args []string) (int, string, bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == "-p" {
			if i+1 < len(args) {
				return i + 1, strings.TrimSpace(args[i+1]), true
			}
			return -1, "", false
		}
		if strings.HasPrefix(args[i], "-p") && len(args[i]) > 2 {
			return i, strings.TrimSpace(args[i][2:]), true
		}
	}
	return -1, "", false
}

func extractGzipFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	reader, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer reader.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, reader); err != nil {
		return err
	}
	return nil
}

func buildRecoveryGoal(goal string, suggestion assist.Suggestion, cmdErr commandError) string {
	builder := strings.Builder{}
	if goal != "" {
		builder.WriteString("Original goal: " + goal + "\n")
	}
	if strings.ToLower(strings.TrimSpace(suggestion.Type)) == "tool" && suggestion.Tool != nil {
		builder.WriteString("Previous tool run failed.\n")
		if suggestion.Tool.Name != "" {
			builder.WriteString("Tool name: " + suggestion.Tool.Name + "\n")
		}
		if suggestion.Tool.Language != "" {
			builder.WriteString("Tool language: " + suggestion.Tool.Language + "\n")
		}
		if suggestion.Tool.Purpose != "" {
			builder.WriteString("Tool purpose: " + suggestion.Tool.Purpose + "\n")
		}
		runLine := strings.TrimSpace(strings.Join(append([]string{suggestion.Tool.Run.Command}, suggestion.Tool.Run.Args...), " "))
		if runLine != "" {
			builder.WriteString("Tool run: " + runLine + "\n")
		}
		if len(suggestion.Tool.Files) > 0 {
			builder.WriteString("Tool files:\n")
			for _, f := range suggestion.Tool.Files {
				if strings.TrimSpace(f.Path) == "" {
					continue
				}
				builder.WriteString("- " + f.Path + "\n")
			}
		}
	} else {
		builder.WriteString("Previous command failed.\n")
		cmdLine := strings.TrimSpace(strings.Join(append([]string{suggestion.Command}, suggestion.Args...), " "))
		if cmdLine != "" {
			builder.WriteString("Command: " + cmdLine + "\n")
		}
	}
	if cmdErr.Err != nil {
		builder.WriteString("Error: " + cmdErr.Err.Error() + "\n")
	}
	if output := firstLines(cmdErr.Result.Output, 3); output != "" {
		builder.WriteString("Output: " + output + "\n")
	}
	builder.WriteString("Provide a recovery suggestion or alternative next step.")
	return builder.String()
}

func buildNextStepsGoal(goal string, suggestion assist.Suggestion) string {
	builder := strings.Builder{}
	if goal != "" {
		builder.WriteString("Original goal: " + goal + "\n")
	}
	if suggestion.Summary != "" {
		builder.WriteString("Last action summary: " + suggestion.Summary + "\n")
	}
	cmdLine := strings.TrimSpace(strings.Join(append([]string{suggestion.Command}, suggestion.Args...), " "))
	if cmdLine != "" {
		builder.WriteString("Last command: " + cmdLine + "\n")
	}
	builder.WriteString("Suggest 1-3 concise next steps or a clarifying question.")
	return builder.String()
}

func normalizeWhoisTarget(target string) (string, bool) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	lower = strings.TrimPrefix(lower, "http://")
	lower = strings.TrimPrefix(lower, "https://")
	lower = strings.SplitN(lower, "/", 2)[0]
	lower = strings.TrimSuffix(lower, ".")
	if strings.HasPrefix(lower, "www.") {
		lower = strings.TrimPrefix(lower, "www.")
	}
	if lower == "" {
		return "", false
	}
	return lower, true
}

func normalizeShellScriptArgs(command string, args []string) []string {
	if len(args) < 2 {
		return args
	}
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd != "bash" && cmd != "sh" && cmd != "zsh" {
		return args
	}
	flag := strings.TrimSpace(args[0])
	if flag != "-c" && flag != "-lc" {
		return args
	}
	script := strings.TrimSpace(strings.Join(args[1:], " "))
	if len(script) >= 2 {
		if (strings.HasPrefix(script, "'") && strings.HasSuffix(script, "'")) ||
			(strings.HasPrefix(script, "\"") && strings.HasSuffix(script, "\"")) {
			script = strings.TrimSpace(script[1 : len(script)-1])
		}
	}
	return []string{flag, script}
}

func extractShellScript(command string, args []string) (string, bool) {
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd != "bash" && cmd != "sh" && cmd != "zsh" {
		return "", false
	}
	if len(args) < 2 {
		return "", false
	}
	flag := strings.TrimSpace(args[0])
	if flag != "-c" && flag != "-lc" {
		return "", false
	}
	return strings.TrimSpace(strings.Join(args[1:], " ")), true
}

func looksLikeFragileShellPipeline(script string) bool {
	script = strings.ToLower(strings.TrimSpace(script))
	if script == "" {
		return false
	}
	// Only block multi-command/pipeline shells; single commands are fine.
	if !strings.Contains(script, "|") && !strings.Contains(script, "&&") && !strings.Contains(script, ";") {
		return false
	}
	// We mainly want to avoid brittle web parsing pipelines.
	hasHTTP := strings.Contains(script, "curl ") || strings.Contains(script, "wget ") || strings.Contains(script, "http://") || strings.Contains(script, "https://")
	if !hasHTTP {
		return false
	}
	hasTextFilters := strings.Contains(script, "grep ") || strings.Contains(script, "sed ") || strings.Contains(script, "awk ") || strings.Contains(script, "cut ") || strings.Contains(script, "sort ")
	if !hasTextFilters {
		return false
	}
	return true
}

func firstLines(text string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	out := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= maxLines {
			break
		}
	}
	return strings.Join(out, " / ")
}

func (r *Runner) resetAssistCommandLoop() {
	r.lastAssistCmdKey = ""
	r.lastAssistCmdSeen = 0
}

func (r *Runner) resetAssistLoopState() {
	r.resetAssistCommandLoop()
	r.lastAssistQuestion = ""
	r.lastAssistQSeen = 0
}

func (r *Runner) guardAssistCommandLoop(command string, args []string) error {
	key := canonicalAssistActionKey(command, args)
	if key == r.lastAssistCmdKey {
		r.lastAssistCmdSeen++
	} else {
		r.lastAssistCmdKey = key
		r.lastAssistCmdSeen = 1
	}
	const maxSameCommandInRow = 2
	if r.lastAssistCmdSeen > maxSameCommandInRow {
		return fmt.Errorf("assistant loop guard: repeated command blocked: %s %s", command, strings.Join(args, " "))
	}
	r.lastAssistQuestion = ""
	r.lastAssistQSeen = 0
	return nil
}

func (r *Runner) guardAssistQuestionLoop(question string) error {
	key := strings.TrimSpace(strings.ToLower(question))
	if key == "" {
		return nil
	}
	if key == r.lastAssistQuestion {
		r.lastAssistQSeen++
	} else {
		r.lastAssistQuestion = key
		r.lastAssistQSeen = 1
	}
	const maxSameQuestionInRow = 1
	if r.lastAssistQSeen > maxSameQuestionInRow {
		return fmt.Errorf("assistant loop guard: repeated question blocked")
	}
	r.resetAssistCommandLoop()
	return nil
}

func sanitizeBrowseArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("assistant returned browse without url")
	}
	candidates := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		// Treat unicode dashes as flags too, to avoid "https://-v" style bugs.
		if isFlagLike(arg) {
			continue
		}
		candidates = append(candidates, arg)
	}
	for _, c := range candidates {
		if looksLikeURLOrHost(c) {
			return []string{c}, nil
		}
	}
	return nil, fmt.Errorf("assistant returned browse without url")
}

func isFlagLike(arg string) bool {
	if arg == "" {
		return false
	}
	// Also treat some common unicode dash variants as flags.
	// Use escapes to keep the source ASCII-only.
	return strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "\u2013") || strings.HasPrefix(arg, "\u2014")
}

func looksLikeURLOrHost(arg string) bool {
	lower := strings.ToLower(strings.TrimSpace(arg))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "://") {
		return true
	}
	// Very small heuristic: hosts normally have a dot or are localhost.
	if lower == "localhost" {
		return true
	}
	if strings.Contains(lower, ".") {
		return true
	}
	// Allow raw IPs.
	for _, r := range lower {
		if (r >= '0' && r <= '9') || r == '.' {
			continue
		}
		return false
	}
	return strings.Count(lower, ".") == 3
}

func (r *Runner) enrichAssistGoal(goal, mode string) string {
	if mode != "recover" && mode != "follow-up" && mode != "next-steps" {
		return goal
	}
	path := strings.TrimSpace(r.lastActionLogPath)
	if path == "" {
		return goal
	}
	if _, err := os.Stat(path); err != nil {
		return goal
	}
	builder := strings.Builder{}
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	builder.WriteString("Context: latest action artifact: ")
	builder.WriteString(path)
	builder.WriteString(". Prefer analyzing this local artifact before repeating the same action.")
	return builder.String()
}

func (r *Runner) recordActionArtifact(logPath string) {
	path := strings.TrimSpace(logPath)
	if path == "" {
		return
	}
	r.lastActionLogPath = path
}

func (r *Runner) recordObservationFromResult(kind string, result exec.CommandResult, err error) {
	command := strings.TrimSpace(result.Command)
	if command == "" {
		return
	}
	excerpt := firstLines(result.Output, 12)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	r.recordObservationWithCommand(kind, command, result.Args, result.LogPath, excerpt, errText, exitCodeFromErr(err))
}

func (r *Runner) recordObservation(kind string, args []string, logPath string, outputExcerpt string, err error) {
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	r.recordObservationWithCommand(kind, kind, args, logPath, outputExcerpt, errText, exitCodeFromErr(err))
}

func (r *Runner) recordObservationWithCommand(kind string, command string, args []string, logPath string, outputExcerpt string, errText string, exitCode int) {
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	manager := r.memoryManager(sessionDir)
	_, _ = manager.RecordObservation(memory.Observation{
		Time:          time.Now().UTC().Format(time.RFC3339),
		Kind:          strings.TrimSpace(kind),
		Command:       strings.TrimSpace(command),
		Args:          args,
		ExitCode:      exitCode,
		Error:         strings.TrimSpace(errText),
		LogPath:       strings.TrimSpace(logPath),
		OutputExcerpt: strings.TrimSpace(outputExcerpt),
	})
}

func (r *Runner) clearActionContext() {
	r.lastActionLogPath = ""
	r.lastBrowseLogPath = ""
}

func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return 124
	}
	var exitErr *goexec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ProcessState != nil {
			return exitErr.ProcessState.ExitCode()
		}
	}
	lower := strings.ToLower(err.Error())
	if idx := strings.LastIndex(lower, "exit status "); idx >= 0 {
		tail := strings.TrimSpace(lower[idx+len("exit status "):])
		end := len(tail)
		for i, r := range tail {
			if r < '0' || r > '9' {
				end = i
				break
			}
		}
		if end > 0 {
			if n, convErr := strconv.Atoi(tail[:end]); convErr == nil {
				return n
			}
		}
	}
	return 1
}

func (r *Runner) updateKnownTargetFromText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if url := extractFirstURL(text); url != "" {
		if normalized, err := normalizeURL(url); err == nil {
			if parsed, err := neturl.Parse(normalized); err == nil && parsed.Host != "" {
				r.lastKnownTarget = parsed.Hostname()
				return
			}
		}
	}
	token := extractHostLikeToken(text)
	if token != "" {
		r.lastKnownTarget = token
	}
}

func (r *Runner) bestKnownTarget() string {
	target := strings.TrimSpace(r.lastKnownTarget)
	if target == "" {
		return ""
	}
	if strings.Contains(target, "://") {
		parsed, err := neturl.Parse(target)
		if err == nil && parsed.Hostname() != "" {
			return parsed.Hostname()
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(target, "."))
}

func (r *Runner) appendConversation(role, content string) {
	content = strings.TrimSpace(content)
	if role == "" || content == "" {
		return
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return
	}
	r.appendChatHistory(artifacts.ChatPath, role, content)
	r.maybeAutoSummarizeChat(sessionDir, artifacts.ChatPath)
}

func (r *Runner) updateTaskFoundation(goal string) {
	goal = collapseWhitespace(strings.TrimSpace(goal))
	if goal == "" {
		return
	}
	if len(goal) > 240 {
		goal = goal[:240]
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return
	}
	existing, err := memory.ReadBullets(artifacts.FocusPath)
	if err != nil {
		existing = nil
	}
	items := []string{goal}
	for _, item := range existing {
		item = collapseWhitespace(strings.TrimSpace(item))
		if item == "" || strings.EqualFold(item, goal) || strings.EqualFold(item, "Not set.") {
			continue
		}
		items = append(items, item)
		if len(items) >= 12 {
			break
		}
	}
	_ = memory.WriteFocus(artifacts.FocusPath, items)
}

func (r *Runner) maybeAutoSummarizeChat(sessionDir, chatPath string) {
	if chatPath == "" {
		return
	}
	manager := r.memoryManager(sessionDir)
	state, err := manager.RecordLog(chatPath)
	if err != nil {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Context tracking failed: %v", err)
		}
		return
	}
	if !manager.ShouldSummarize(state) {
		return
	}
	if r.cfg.Permissions.Level == "readonly" {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Auto-summarize skipped (readonly)")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("summarize")
	defer stopIndicator()
	if err := manager.Summarize(ctx, r.summaryGenerator(), "chat"); err != nil {
		r.logger.Printf("Auto-summarize failed: %v", err)
		return
	}
	if r.cfg.UI.Verbose {
		r.logger.Printf("Auto-summary updated")
	}
}

func isPlaceholderCommand(cmd string) bool {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	switch cmd {
	case "scan", "recon", "enumerate", "probe", "test":
		return true
	default:
		return false
	}
}

func isSummaryIntent(goal string) bool {
	if goal == "" {
		return false
	}
	lower := strings.ToLower(goal)
	hints := []string{"summary", "summarize", "what did you find", "findings", "what is it about", "tell me what", "report"}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func shouldStartBaselineScan(answer string) bool {
	normalized := strings.ToLower(strings.TrimSpace(answer))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "scan") && (strings.Contains(normalized, "start") || strings.Contains(normalized, "first") || strings.HasPrefix(normalized, "yes")) {
		return true
	}
	if strings.Contains(normalized, "non intrusive") && strings.Contains(normalized, "start") {
		return true
	}
	return false
}

func extractHostLikeToken(text string) string {
	candidates := strings.Fields(strings.ToLower(text))
	for _, token := range candidates {
		token = strings.Trim(token, "\"'()[]{}<>.,;:")
		if token == "" {
			continue
		}
		if strings.Contains(token, "/") || strings.Contains(token, ":") {
			continue
		}
		if strings.Count(token, ".") >= 1 && !strings.HasPrefix(token, ".") && !strings.HasSuffix(token, ".") {
			return token
		}
	}
	return ""
}

func isBenignNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	var cmdErr commandError
	if !errors.As(err, &cmdErr) {
		return false
	}
	if cmdErr.Err == nil || !strings.Contains(strings.ToLower(cmdErr.Err.Error()), "exit status 1") {
		return false
	}
	command := strings.ToLower(strings.TrimSpace(cmdErr.Result.Command))
	switch command {
	case "grep", "rg":
		return strings.TrimSpace(cmdErr.Result.Output) == ""
	case "bash", "sh":
		if len(cmdErr.Result.Args) == 0 {
			return false
		}
		joined := strings.ToLower(strings.Join(cmdErr.Result.Args, " "))
		return strings.Contains(joined, "grep") && strings.TrimSpace(cmdErr.Result.Output) == ""
	default:
		return false
	}
}

func isAssistRepeatedGuard(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "assistant loop guard: repeated command blocked") ||
		strings.Contains(lower, "assistant loop guard: repeated question blocked")
}

func shouldUseDefaultChoice(answer, question string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	question = strings.ToLower(strings.TrimSpace(question))
	if answer == "" || question == "" {
		return false
	}
	defaultSignals := []string{"default", "go ahead", "proceed", "use that", "continue", "yes"}
	hasDefault := false
	for _, signal := range defaultSignals {
		if strings.Contains(answer, signal) {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		return false
	}
	optionSignals := []string{"default", "option", "wordlist", "password list", "profile", "mode"}
	for _, signal := range optionSignals {
		if strings.Contains(question, signal) {
			return true
		}
	}
	return false
}

func normalizeAssistantOutput(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = ansiEscapePattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\t", "    ")
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	width := 0
	if isTerminalStdout() {
		width = terminalWidth() - 2
	}
	return wrapTextForTerminal(text, width)
}

var ansiEscapePattern = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

func wrapTextForTerminal(text string, width int) string {
	if width < 10 {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRightFunc(line, unicode.IsSpace)
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapLine(line, width)...)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func wrapLine(line string, width int) []string {
	if width < 10 {
		return []string{line}
	}
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	out := []string{}
	for len(runes) > width {
		cut := width
		for i := width; i > 0; i-- {
			if unicode.IsSpace(runes[i-1]) {
				cut = i
				break
			}
		}
		if cut == 0 {
			cut = width
		}
		segment := strings.TrimRightFunc(string(runes[:cut]), unicode.IsSpace)
		if segment != "" {
			out = append(out, segment)
		}
		runes = trimLeadingSpaceRunes(runes[cut:])
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}

func trimLeadingSpaceRunes(r []rune) []rune {
	for len(r) > 0 && unicode.IsSpace(r[0]) {
		r = r[1:]
	}
	return r
}

func isTerminalStdout() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func looksLikeChat(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return false
	}
	if looksLikeAction(trimmed) {
		return false
	}
	if strings.Contains(trimmed, "?") {
		return true
	}
	if hasPrefixOneOf(trimmed, "hello", "hi", "hey", "good morning", "good afternoon", "good evening") {
		return true
	}
	if hasPrefixOneOf(trimmed, "my name is", "i am", "i'm") {
		return true
	}
	if hasPrefixOneOf(trimmed, "who ", "what ", "where ", "why ", "how ", "can you", "could you", "would you", "tell me", "explain", "describe") {
		return true
	}
	return true
}

func looksLikeAction(text string) bool {
	if hasURLHint(text) {
		return true
	}
	if looksLikeFileQuery(text) {
		return true
	}
	verbs := map[string]struct{}{
		"scan": {}, "enumerate": {}, "list": {}, "show": {}, "find": {}, "run": {}, "check": {}, "exploit": {},
		"test": {}, "probe": {}, "search": {}, "ping": {}, "nmap": {}, "curl": {}, "msf": {}, "msfconsole": {},
		"netstat": {}, "ls": {}, "whoami": {}, "cat": {}, "dir": {}, "open": {}, "dump": {}, "inspect": {}, "analyze": {},
		"investigate": {}, "recon": {}, "crawl": {}, "browse": {}, "report": {}, "summarize": {},
	}
	for _, token := range splitTokens(text) {
		if _, ok := verbs[token]; ok {
			return true
		}
	}
	return false
}

func hasPrefixOneOf(text string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func splitTokens(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= '0' && r <= '9' {
			return false
		}
		return true
	})
}

func looksLikeFileQuery(text string) bool {
	lower := strings.ToLower(text)
	if !hasFileHint(lower) {
		return false
	}
	if strings.Contains(lower, "?") {
		return true
	}
	if hasPrefixOneOf(lower, "open", "read", "show", "view", "display", "cat", "print") {
		return true
	}
	if strings.Contains(lower, "inside") || strings.Contains(lower, "contents") || strings.Contains(lower, "content") {
		return true
	}
	if strings.Contains(lower, "what is in") || strings.Contains(lower, "what's in") {
		return true
	}
	return false
}

func hasFileHint(text string) bool {
	if strings.Contains(text, "readme") {
		return true
	}
	if strings.Contains(text, "/") || strings.Contains(text, "\\") {
		return true
	}
	extensions := []string{".md", ".txt", ".log", ".json", ".yaml", ".yml", ".toml", ".ini", ".conf", ".cfg", ".go", ".py", ".sh", ".js", ".ts", ".zip", ".7z"}
	for _, ext := range extensions {
		if strings.Contains(text, ext) {
			return true
		}
	}
	return false
}

func hasURLHint(text string) bool {
	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		return true
	}
	for _, token := range strings.Fields(text) {
		clean := strings.Trim(token, " \t\r\n\"'()[]{}<>.,;:")
		if strings.Count(clean, ".") >= 1 && len(clean) >= 4 {
			if strings.HasPrefix(clean, ".") || strings.HasSuffix(clean, ".") {
				continue
			}
			return true
		}
	}
	return false
}

func extractFirstURL(text string) string {
	if text == "" {
		return ""
	}
	if match := findURLWithScheme(text); match != "" {
		return match
	}
	for _, token := range strings.Fields(text) {
		clean := strings.Trim(token, " \t\r\n\"'()[]{}<>.,;:")
		if clean == "" {
			continue
		}
		if strings.Contains(clean, "://") {
			return clean
		}
		if strings.HasPrefix(strings.ToLower(clean), "www.") || strings.Count(clean, ".") >= 1 {
			if strings.HasPrefix(clean, ".") || strings.HasSuffix(clean, ".") {
				continue
			}
			return clean
		}
	}
	return ""
}

func findURLWithScheme(text string) string {
	start := strings.Index(text, "http://")
	if start == -1 {
		start = strings.Index(text, "https://")
	}
	if start == -1 {
		return ""
	}
	rest := text[start:]
	end := len(rest)
	for i, r := range rest {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			end = i
			break
		}
	}
	candidate := strings.Trim(rest[:end], "\"'()[]{}<>.,;:")
	return candidate
}

func shouldAutoBrowse(text string) bool {
	url := extractFirstURL(text)
	if url == "" {
		return false
	}
	lower := strings.ToLower(text)
	blockers := []string{"scan", "enumerate", "ffuf", "gobuster", "dirsearch", "nmap", "nikto", "nuclei", "exploit", "attack"}
	for _, word := range blockers {
		if strings.Contains(lower, word) {
			return false
		}
	}
	if strings.TrimSpace(lower) == strings.ToLower(url) || strings.TrimSpace(lower) == "www."+strings.TrimPrefix(strings.ToLower(url), "www.") {
		return true
	}
	infoHints := []string{"what", "tell me", "about", "overview", "summarize", "whois", "info", "information", "website", "site", "page"}
	for _, hint := range infoHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// Moved: UI/prompt/LLM indicator helpers live in ui.go.
