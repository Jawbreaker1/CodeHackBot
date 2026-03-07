package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
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
	temperature, maxTokens := r.llmRoleOptions("assist", 0.15, 1200)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("ask")
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: "You are BirdHackBot, a security testing assistant operating in an authorized lab owned by the user. Never claim to be Claude, OpenAI, Anthropic, or any other assistant identity. Provide direct help within scope, including inspecting local files or assisting with encrypted files the user created. Assume the standard Kali Linux toolchain is available, including Metasploit (msfconsole), and suggest/use those tools when suitable to the goal and scope. For vulnerability/CVE statements, do not rely on memory alone: require source-backed evidence from tooling or captured advisories, and clearly label anything not yet validated as unverified. If access requires a password, ask for it; if asked to crack a password, proceed only when the user confirms authorization for that file. Do not emit tool-call markup; respond in plain text. If web access is needed, ask the user to run /browse with the URL (it requires approval). Answer clearly and concisely and ask follow-up questions when needed.",
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
	msg = r.enforceEvidenceClaims(msg)
	safePrintln(msg)
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
	temperature, maxTokens := r.llmRoleOptions("summarize", 0.1, 1000)
	primary := memory.LLMSummarizer{
		Client:      llm.NewLMStudioClient(r.cfg),
		Model:       r.cfg.LLM.Model,
		Temperature: r.float32Ptr(temperature),
		MaxTokens:   r.intPtr(maxTokens),
	}
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

func (r *Runner) llmRoleOptions(role string, fallbackTemp float32, fallbackMaxTokens int) (float32, int) {
	return r.cfg.ResolveLLMRoleOptions(role, fallbackTemp, fallbackMaxTokens)
}

func (r *Runner) float32Ptr(v float32) *float32 {
	return &v
}

func (r *Runner) intPtr(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func (r *Runner) handleAssistGoal(goal string, dryRun bool) error {
	trimmedGoal := strings.TrimSpace(goal)
	if trimmedGoal != "" {
		r.updateKnownTargetFromText(trimmedGoal)
		r.updateTaskFoundation(trimmedGoal)
		r.pendingAssistGoal = ""
		r.pendingAssistQ = ""
		r.assistObjectiveMet = false
		r.resetAssistLoopState()
	}
	if !dryRun && isSummaryIntent(trimmedGoal) {
		if artifactPath := r.summaryArtifactPath(trimmedGoal); artifactPath != "" {
			if r.cfg.UI.Verbose {
				r.logger.Printf("Using artifact for summary: %s", artifactPath)
			}
			return r.summarizeFromLatestArtifact(trimmedGoal)
		}
	}
	err := r.handleAssistGoalWithMode(goal, dryRun, "")
	if err == nil {
		r.ensureActionGoalTerminalState(trimmedGoal, dryRun)
	}
	return err
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
		if isOperatorInterrupted(err) {
			r.handleOperatorInterrupt(strings.TrimSpace(goal))
			return nil
		}
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
	if r.openLikeAssistLoopEnabled() {
		budget.enableLongHorizon()
	}
	r.startAssistRuntime(goal, mode, budget)
	defer r.clearAssistRuntime()
	if !dryRun {
		proceed, err := r.maybeConfirmGoalExecution(goal, dryRun)
		if err != nil {
			return err
		}
		if !proceed {
			return nil
		}
	}
	repeatedGuardHits := 0
	stepMode := mode
	lastCommand := assist.Suggestion{}
	for {
		r.updateAssistRuntime(stepMode, budget)
		if budget.exhausted() {
			if r.openLikeAssistLoopEnabled() && r.isActionGoal(goal) && !r.assistObjectiveMet {
				if budget.extendForPersistence(3, "open-like persistence extension") {
					if r.cfg.UI.Verbose {
						r.logger.Printf("Open-like mode extended step budget to %d/%d while objective remains unmet.", budget.used, budget.currentCap)
					}
					continue
				}
			}
			if r.cfg.UI.Verbose {
				r.logger.Printf("Reached step budget (%d/%d).", budget.used, budget.currentCap)
			}
			if !dryRun {
				if r.tryConcludeGoalFromArtifacts(goal) {
					r.maybeFinalizeReport(goal, dryRun)
					return nil
				}
				safePrintln("Reached dynamic step budget without a final answer. Suggesting next steps.")
			}
			if !dryRun && lastCommand.Type == "command" {
				r.maybeSuggestNextSteps(goal, lastCommand)
			}
			if !dryRun && r.openLikeAssistLoopEnabled() && r.isActionGoal(goal) && !r.assistObjectiveMet {
				msg := "Objective not met yet from current evidence. Continue with more attempts by replying `continue`, or provide a new strategy/wordlist/tool hint."
				safePrintln(msg)
				r.appendConversation("Assistant", msg)
				r.pendingAssistGoal = goal
				r.pendingAssistQ = msg
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
		var normalizeNote string
		suggestion, normalizeNote = r.normalizeAssistDecision(suggestion)
		if normalizeNote != "" && r.cfg.UI.Verbose {
			r.logger.Printf("%s", normalizeNote)
		}
		if suggestion.Type == "noop" && strings.TrimSpace(goal) != "" {
			budget.consume("noop -> clarify")
			budget.onStall("noop suggestion")
			r.updateAssistRuntime("recover", budget)
			return r.handleAssistNoop(goal, dryRun)
		}
		if err := r.validateAssistDecisionContract(suggestion); err != nil {
			budget.consume("invalid decision contract")
			budget.onStall("invalid decision contract")
			r.updateAssistRuntime("recover", budget)
			if r.cfg.UI.Verbose {
				r.logger.Printf("Decision contract violation: %v", err)
			}
			if r.handleAssistCommandFailure(goal, suggestion, err) {
				if shouldPauseAfterHandledFailure(dryRun, r.pendingAssistGoal, r.pendingAssistQ) {
					r.maybeEmitGoalSummary(goal, dryRun)
					r.maybeFinalizeReport(goal, dryRun)
					return nil
				}
				stepMode = "recover"
				continue
			}
			return err
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
			if strings.TrimSpace(r.pendingAssistQ) != "" && strings.TrimSpace(r.pendingAssistGoal) == "" {
				// A question was raised while executing plan steps; keep goal pending so follow-up can resume.
				r.pendingAssistGoal = goal
			}
			r.maybeEmitGoalSummary(goal, dryRun)
			r.maybeFinalizeReport(goal, dryRun)
			return nil
		}
		if suggestion.Type == "complete" {
			budget.consume("goal completed")
			budget.onProgress("assistant completion")
			r.updateAssistRuntime(stepMode, budget)
			if err := r.executeAssistSuggestion(suggestion, dryRun); err != nil {
				if isOperatorInterrupted(err) {
					r.handleOperatorInterrupt(strings.TrimSpace(goal))
					r.maybeEmitGoalSummary(goal, dryRun)
					r.maybeFinalizeReport(goal, dryRun)
					return nil
				}
				budget.onStall("invalid completion contract")
				r.updateAssistRuntime("recover", budget)
				if r.handleAssistCommandFailure(goal, suggestion, err) {
					if shouldPauseAfterHandledFailure(dryRun, r.pendingAssistGoal, r.pendingAssistQ) {
						r.maybeEmitGoalSummary(goal, dryRun)
						r.maybeFinalizeReport(goal, dryRun)
						return nil
					}
					stepMode = "recover"
					continue
				}
				return err
			}
			r.maybeFinalizeReport(goal, dryRun)
			return nil
		}
		beforeObs := r.latestObservationSignature()
		actionKey := ""
		if suggestion.Type == "command" {
			actionKey = canonicalAssistActionKey(suggestion.Command, suggestion.Args)
		}
		if err := r.executeAssistSuggestion(suggestion, dryRun); err != nil {
			if isOperatorInterrupted(err) {
				r.handleOperatorInterrupt(strings.TrimSpace(goal))
				r.maybeEmitGoalSummary(goal, dryRun)
				r.maybeFinalizeReport(goal, dryRun)
				return nil
			}
			if isAssistRepeatedGuard(err) {
				if !dryRun && r.tryConcludeGoalFromArtifacts(goal) {
					r.maybeFinalizeReport(goal, dryRun)
					return nil
				}
				repeatedGuardHits++
				budget.onStall("repeated step blocked")
				r.updateAssistRuntime("recover", budget)
				if repeatedGuardHits >= 2 {
					budget.consume("repeated loop recovery")
					r.updateAssistRuntime("recover", budget)
					if !dryRun {
						if r.cfg.UI.Verbose {
							r.logger.Printf("Repeated step loop detected; attempting automated recovery.")
						} else {
							safePrintln("Repeated step loop detected; attempting automated recovery.")
						}
					}
					if r.handleAssistCommandFailure(goal, suggestion, err) {
						if shouldPauseAfterHandledFailure(dryRun, r.pendingAssistGoal, r.pendingAssistQ) {
							r.maybeEmitGoalSummary(goal, dryRun)
							r.maybeFinalizeReport(goal, dryRun)
							return nil
						}
						repeatedGuardHits = 0
						stepMode = "recover"
						continue
					}
					msg := "Repeated step loop detected. Pausing for guidance: share the exact next action/target and I will continue."
					safePrintln(msg)
					r.appendConversation("Assistant", msg)
					r.pendingAssistGoal = goal
					r.pendingAssistQ = msg
					return nil
				}
				if !dryRun {
					if r.cfg.UI.Verbose {
						r.logger.Printf("Repeated step detected; requesting an alternative action.")
					} else {
						safePrintln("Repeated step detected; asking assistant for an alternative action.")
					}
				}
				stepMode = "recover"
				continue
			}
			if r.openLikeAssistLoopEnabled() && !dryRun {
				if r.tryImmediateAssistRepair(goal, suggestion, err) {
					budget.consume("step repaired")
					budget.onProgress("immediate repair succeeded")
					r.updateAssistRuntime("recover", budget)
					if r.tryConcludeGoalFromArtifacts(goal) {
						r.assistObjectiveMet = true
						r.maybeFinalizeReport(goal, dryRun)
						return nil
					}
					stepMode = "recover"
					continue
				}
			}
			budget.consume("step failed")
			budget.onStall("step execution failed")
			r.updateAssistRuntime("recover", budget)
			if r.handleAssistCommandFailure(goal, suggestion, err) {
				if shouldPauseAfterHandledFailure(dryRun, r.pendingAssistGoal, r.pendingAssistQ) {
					r.maybeEmitGoalSummary(goal, dryRun)
					r.maybeFinalizeReport(goal, dryRun)
					return nil
				}
				stepMode = "recover"
				continue
			}
			r.maybeFinalizeReport(goal, dryRun)
			return err
		}
		if dryRun {
			return nil
		}
		repeatedGuardHits = 0
		budget.consume("step executed")
		nextMode := "execute-step"
		switch suggestion.Type {
		case "question":
			budget.onProgress("awaiting user input")
		case "command", "tool":
			afterObs := r.latestObservationSignature()
			if progressed, reason := budget.trackProgress(actionKey, beforeObs, afterObs); progressed {
				budget.onProgress(reason)
			} else {
				budget.onStall(reason)
				nextMode = "recover"
				if r.cfg.UI.Verbose {
					r.logger.Printf("No new evidence from step; requesting an alternative action.")
				}
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
			if autoAnswer, ok := autoAssistFollowUpAnswer(suggestion.Question); ok {
				if r.cfg.UI.Verbose {
					r.logger.Printf("Auto-answering assistant question: %s", autoAnswer)
				}
				goal = r.composeAssistFollowUpGoal(goal, suggestion.Question, autoAnswer)
				r.pendingAssistGoal = ""
				r.pendingAssistQ = ""
				stepMode = "follow-up"
				continue
			}
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
		stepMode = nextMode
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
		safePrintf("Step %d/%d: %s\n", stepNum, maxSteps, desc)
		return
	}
	safePrintf("Step %d: %s\n", stepNum, desc)
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
		return "validating completion claim"
	default:
		return ""
	}
}

func shouldPauseAfterHandledFailure(dryRun bool, pendingGoal, pendingQuestion string) bool {
	if dryRun {
		return true
	}
	return strings.TrimSpace(pendingGoal) != "" || strings.TrimSpace(pendingQuestion) != ""
}

func (r *Runner) handleAssistNoop(goal string, dryRun bool) error {
	clarifyGoal := fmt.Sprintf("Original goal: %s\nThe previous suggestion was noop. Provide one concrete next step. If details are missing, ask one concise clarifying question.", goal)
	stopIndicator := r.startLLMIndicatorIfAllowed("assist clarify")
	suggestion, err := r.getAssistSuggestion(clarifyGoal, "recover")
	stopIndicator()
	if err != nil {
		r.pendingAssistGoal = goal
		safePrintln("I need one more detail to continue. Share what target/path/url to act on.")
		return nil
	}
	if suggestion.Type == "noop" {
		r.pendingAssistGoal = goal
		safePrintln("I need one more detail to continue. Share what target/path/url to act on.")
		return nil
	}
	if err := r.executeAssistSuggestion(suggestion, dryRun); err != nil {
		if isOperatorInterrupted(err) {
			r.handleOperatorInterrupt(strings.TrimSpace(goal))
			return nil
		}
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
	combined := r.composeAssistFollowUpGoal(goal, prevQuestion, answer)
	return r.handleAssistGoalWithMode(combined, false, "follow-up")
}

func (r *Runner) composeAssistFollowUpGoal(goal, previousQuestion, answer string) string {
	combined := fmt.Sprintf("Original goal: %s\nUser answer to previous assistant question: %s\nContinue the task using available tools.", goal, answer)
	if strings.TrimSpace(previousQuestion) != "" {
		combined += fmt.Sprintf("\nAssistant previous question: %s", previousQuestion)
		combined += "\nDo not repeat the same clarifying question if the answer already resolves it; pick a concrete next action."
	}
	if shouldUseDefaultChoice(answer, previousQuestion) {
		combined += "\nUser explicitly chose the default option. Select a safe default available in the current environment and proceed."
	}
	if indicatesMissingCreation(answer) && isWriteCreationIntent(goal) {
		combined += "\nUser reports the requested output file is still missing. The next step must create/write the requested file now; do not repeat list/read checks."
	}
	if target := strings.TrimSpace(r.lastKnownTarget); target != "" {
		combined += fmt.Sprintf("\nCurrent remembered target: %s", target)
	}
	return combined
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
		question := normalizeAssistantOutput(suggestion.Question)
		question = r.enforceEvidenceClaims(question)
		handoff := formatAssistQuestionForUser(question, suggestion.Summary)
		safePrintln(handoff)
		r.appendConversation("Assistant", handoff)
		r.pendingAssistQ = suggestion.Question
		return nil
	case "noop":
		if r.cfg.UI.Verbose {
			r.logger.Printf("Assistant has no suggestion")
		}
		return nil
	case "complete":
		if err := r.validateAssistCompletionContract(suggestion); err != nil {
			return err
		}
		final := strings.TrimSpace(suggestion.Final)
		if final == "" {
			final = strings.TrimSpace(suggestion.Summary)
		}
		if final == "" {
			final = "(completed)"
		}
		final = normalizeAssistantOutput(final)
		final = r.enforceEvidenceClaims(final)
		final = renderCompletionMessage(suggestion, final)
		safePrintln(final)
		r.appendConversation("Assistant", final)
		r.assistObjectiveMet = suggestion.ObjectiveMet != nil && *suggestion.ObjectiveMet
		sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
		_ = r.appendTaskJournalEntry(sessionDir, taskJournalEntry{
			Kind:         "complete",
			Result:       map[bool]string{true: "ok", false: "error"}[r.assistObjectiveMet],
			Decision:     "step_complete",
			EvidenceRefs: append([]string{}, suggestion.EvidenceRefs...),
			Notes:        strings.TrimSpace(suggestion.WhyMet),
			Excerpt:      strings.TrimSpace(final),
		})
		_ = r.writeResultSnapshot(r.currentAssistGoal(), suggestion)
		r.refreshFocusFromRecentState(sessionDir, r.currentAssistGoal())
		r.pendingAssistGoal = ""
		r.pendingAssistQ = ""
		return nil
	case "tool":
		if suggestion.Tool == nil {
			return fmt.Errorf("assistant returned tool without tool spec")
		}
		approved, err := r.maybeConfirmAssistExecution(suggestion, dryRun)
		if err != nil {
			return err
		}
		if !approved {
			return executionApprovalRequiredError()
		}
		return r.executeToolSuggestion(*suggestion.Tool, dryRun)
	case "plan":
		r.resetAssistLoopState()
		return r.handlePlanSuggestion(suggestion, dryRun)
	case "command":
		if suggestion.Command == "" {
			return fmt.Errorf("assistant returned empty command")
		}
		var contractErr error
		suggestion.Command, suggestion.Args, contractErr = normalizeAssistCommandContract(suggestion.Command, suggestion.Args)
		if contractErr != nil {
			return contractErr
		}
		suggestion.Args = normalizeShellScriptArgs(suggestion.Command, suggestion.Args)
		if strings.EqualFold(suggestion.Command, "write_file") || strings.EqualFold(suggestion.Command, "write") {
			if len(suggestion.Args) < 2 {
				return fmt.Errorf("assistant command contract: write_file requires path and content arguments (non-interactive mode)")
			}
		}
		if script, ok := extractShellScript(suggestion.Command, suggestion.Args); ok && looksLikeFragileShellPipeline(script) {
			return fmt.Errorf("fragile shell pipeline blocked: prefer internal tools (/crawl, /browse, /parse_links, /read_file, /list_dir) or return type=tool to build a helper instead of bash pipelines")
		}
		if reason, blocked := assistInteractiveCommandReason(suggestion.Command, suggestion.Args); blocked {
			return fmt.Errorf("assistant command contract: interactive command blocked: %s", reason)
		}
		if err := r.enforceRetryModifiedCommandContract(suggestion); err != nil {
			return err
		}
		if err := r.enforceAssistTargetPivotContract(suggestion); err != nil {
			return err
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
	approved, err := r.maybeConfirmAssistExecution(suggestion, dryRun)
	if err != nil {
		return err
	}
	if !approved {
		return executionApprovalRequiredError()
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
	if strings.EqualFold(suggestion.Command, "report") {
		return r.handleReport(suggestion.Args)
	}
	if strings.HasPrefix(strings.ToLower(suggestion.Command), "http") && len(suggestion.Args) == 0 {
		return r.handleBrowse([]string{suggestion.Command})
	}
	args := append([]string{suggestion.Command}, suggestion.Args...)
	runErr := r.handleRun(args)
	if isBenignNoMatchError(runErr) {
		r.logger.Printf("No matches found for this step; continuing.")
		return nil
	}
	return runErr
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
		safePrintln("Plan:")
		safePrintln(planText)
		r.appendConversation("Assistant", planText)
	}
	if len(suggestion.Steps) > 0 {
		safePrintln("Plan steps:")
		steps := suggestion.Steps
		maxSteps := r.assistMaxSteps()
		if maxSteps > 0 && len(steps) > maxSteps {
			steps = steps[:maxSteps]
		}
		for i, step := range steps {
			safePrintf("%d) %s\n", i+1, step)
		}
		r.appendConversation("Assistant", "Plan steps: "+strings.Join(steps, " | "))
		suggestion.Steps = steps
	}
	goal := strings.TrimSpace(r.assistRuntime.Goal)
	if goal == "" {
		goal = strings.TrimSpace(r.pendingAssistGoal)
	}
	if !dryRun {
		proceed, err := r.maybeConfirmComplexPlanExecution(goal, suggestion.Steps, planText)
		if err != nil {
			return err
		}
		if !proceed {
			return nil
		}
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
	lastResult := assist.Suggestion{}
	pausedForQuestion := false
	for i, step := range suggestion.Steps {
		r.logger.Printf("Executing step %d/%d: %s", i+1, len(suggestion.Steps), step)
		stopIndicator := r.startLLMIndicatorIfAllowed("assist")
		result, err := r.getAssistSuggestion(step, "execute-step")
		stopIndicator()
		if err != nil {
			return err
		}
		if err := r.executeAssistSuggestion(result, false); err != nil {
			if isOperatorInterrupted(err) {
				r.handleOperatorInterrupt(strings.TrimSpace(goal))
				return nil
			}
			if r.handleAssistCommandFailure(step, result, err) {
				if shouldPauseAfterHandledFailure(false, r.pendingAssistGoal, r.pendingAssistQ) {
					return nil
				}
				if strings.TrimSpace(goal) != "" && r.tryConcludeGoalFromArtifacts(goal) {
					if r.cfg.UI.Verbose {
						r.logger.Printf("Plan objective satisfied after recovery at step %d.", i+1)
					}
					return nil
				}
				if r.cfg.UI.Verbose {
					r.logger.Printf("Plan step %d recovered; continuing remaining plan steps.", i+1)
				}
				continue
			}
			return err
		}
		lastResult = result
		if r.tryConcludeGoalFromArtifacts(goal) {
			if r.cfg.UI.Verbose {
				r.logger.Printf("Plan objective satisfied after step %d; skipping remaining steps.", i+1)
			}
			return nil
		}
		if result.Type == "question" {
			r.logger.Printf("Plan paused for user input. Continue after answering.")
			pausedForQuestion = true
			break
		}
	}
	if pausedForQuestion || dryRun {
		return nil
	}
	if r.tryConcludeGoalFromArtifacts(goal) {
		return nil
	}
	if strings.TrimSpace(goal) != "" {
		resolved, err := r.runBoundedPostPlanRecovery(goal)
		if err != nil {
			return err
		}
		if resolved || strings.TrimSpace(r.pendingAssistQ) != "" || strings.TrimSpace(r.pendingAssistGoal) != "" {
			return nil
		}
	}
	msg := "Plan finished, but objective is not yet verified from evidence. Reviewing latest output and suggesting next steps."
	safePrintln(msg)
	r.appendConversation("Assistant", msg)
	if strings.TrimSpace(goal) != "" {
		r.pendingAssistGoal = goal
		r.pendingAssistQ = msg
	}
	if strings.TrimSpace(r.summaryArtifactPath(goal)) != "" {
		if err := r.summarizeFromLatestArtifact(goal); err != nil && r.cfg.UI.Verbose {
			r.logger.Printf("Plan-end summary failed: %v", err)
		}
	}
	if lastResult.Type == "command" || lastResult.Type == "tool" {
		r.maybeSuggestNextSteps(goal, lastResult)
	}
	return nil
}

func (r *Runner) runBoundedPostPlanRecovery(goal string) (bool, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" || !looksLikeAction(goal) {
		return false, nil
	}
	maxAttempts := r.assistPostPlanRecoveryAttempts()
	if maxAttempts <= 0 {
		return false, nil
	}
	if r.cfg.UI.Verbose {
		r.logger.Printf("Plan objective not yet verified; entering bounded recovery (%d step(s)).", maxAttempts)
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if r.tryConcludeGoalFromArtifacts(goal) {
			if r.cfg.UI.Verbose {
				r.logger.Printf("Post-plan recovery verified objective at attempt %d.", attempt)
			}
			return true, nil
		}
		if err := r.handleAssistSingleStep(goal, false, "recover"); err != nil {
			return false, err
		}
		if strings.TrimSpace(r.pendingAssistQ) != "" || strings.TrimSpace(r.pendingAssistGoal) != "" {
			return true, nil
		}
	}
	if r.tryConcludeGoalFromArtifacts(goal) {
		return true, nil
	}
	return false, nil
}

func (r *Runner) enforceRetryModifiedCommandContract(suggestion assist.Suggestion) error {
	if !strings.EqualFold(strings.TrimSpace(suggestion.Type), "command") {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(suggestion.Decision), "retry_modified") {
		return nil
	}
	lastObs, ok := r.latestObservation()
	if !ok || lastObs.ExitCode == 0 {
		return nil
	}
	prevKey := canonicalAssistActionKey(lastObs.Command, lastObs.Args)
	nextKey := canonicalAssistActionKey(suggestion.Command, suggestion.Args)
	if prevKey == "" || nextKey == "" {
		return nil
	}
	if prevKey == nextKey {
		return fmt.Errorf("retry_modified contract: action unchanged after failure; choose pivot_strategy or materially change the action")
	}
	return nil
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
	return isLikelyURLOrHostToken(strings.TrimSpace(arg))
}

func (r *Runner) enrichAssistGoal(goal, mode string) string {
	goal = strings.TrimSpace(goal)
	runtimeGoal := strings.TrimSpace(r.assistRuntime.Goal)
	includeRuntimeObjective := runtimeGoal != "" && !sameAssistGoal(runtimeGoal, goal)
	includeRecoveryContext := mode == "recover" || mode == "follow-up" || mode == "next-steps"
	includeChecklist := includeRecoveryContext || includeRuntimeObjective
	checklistGoal := goal
	if checklistGoal == "" {
		checklistGoal = runtimeGoal
	}
	checklist := []string{}
	if includeChecklist {
		checklist = objectiveChecklist(checklistGoal)
	}

	directive := ""
	if includeRecoveryContext {
		directive = r.recoveryDirectiveForGoal(goal, mode)
	}
	if mode == "recover" && strings.TrimSpace(r.lastAssistCmdKey) != "" {
		repeatDirective := "Recovery directive: previous command was blocked as repeated (" + strings.TrimSpace(r.lastAssistCmdKey) + "). Propose a different next action; do not repeat that same command."
		if strings.TrimSpace(directive) == "" {
			directive = repeatDirective
		} else {
			directive = strings.TrimSpace(directive) + "\n" + repeatDirective
		}
	}
	path := ""
	if includeRecoveryContext {
		path = strings.TrimSpace(r.lastActionLogPath)
	}
	if !includeRuntimeObjective && path == "" && directive == "" && len(checklist) == 0 {
		return goal
	}
	builder := strings.Builder{}
	builder.WriteString(goal)
	if includeRuntimeObjective {
		builder.WriteString("\n")
		builder.WriteString("Session objective: ")
		builder.WriteString(runtimeGoal)
		if mode == "execute-step" {
			builder.WriteString("\n")
			builder.WriteString("Execution rule: Keep the session objective primary while executing this step. Do not lock onto a specific host/service identity without concrete evidence. If identity remains uncertain, run one disambiguation action or ask one precise clarifying question.")
		}
	}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			builder.WriteString("\n")
			builder.WriteString("Context: latest action artifact: ")
			builder.WriteString(path)
			builder.WriteString(". Prefer analyzing this local artifact before repeating the same action.")
		}
	}
	if strings.TrimSpace(directive) != "" {
		builder.WriteString("\n")
		builder.WriteString(strings.TrimSpace(directive))
	}
	if len(checklist) > 0 {
		builder.WriteString("\n")
		builder.WriteString("Objective completion checklist:\n")
		for _, item := range checklist {
			builder.WriteString("- " + item + "\n")
		}
		builder.WriteString("Only return type=complete when the checklist is satisfied by concrete evidence refs.")
	}
	return builder.String()
}

func sameAssistGoal(a, b string) bool {
	a = strings.ToLower(collapseWhitespace(strings.TrimSpace(a)))
	b = strings.ToLower(collapseWhitespace(strings.TrimSpace(b)))
	return a != "" && b != "" && a == b
}
