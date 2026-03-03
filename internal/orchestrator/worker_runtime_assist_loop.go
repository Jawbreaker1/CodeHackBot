package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func runWorkerAssistTask(ctx context.Context, manager *Manager, cfg WorkerRunConfig, task TaskSpec, action TaskAction, scopePolicy *ScopePolicy, runScope Scope, workDir string) error {
	assistantModel, assistMode, assistant, err := cfg.resolveAssistantBuilder()()
	if err != nil {
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistUnavailable, nil)
		return err
	}

	goal := strings.TrimSpace(action.Prompt)
	if goal == "" {
		goal = strings.TrimSpace(task.Goal)
	}
	if goal == "" {
		err := fmt.Errorf("assist action missing goal/prompt")
		_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistNoAction, nil)
		return err
	}
	contextEnvelope := newWorkerAssistContextEnvelope(cfg, task, goal)
	var observations []string
	var priorEnvelope *workerAssistContextEnvelope
	mode := "execute-step"
	toolCalls := 0
	actionSteps := 0
	defer func() {
		contextEnvelope.recordObservationSnapshot(observations)
		contextEnvelope.finalizeRetryResetSignals(priorEnvelope)
		if summary := contextEnvelope.finalizeAttemptDelta(priorEnvelope); summary != "" {
			_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":               "attempt delta summary",
				"step":                  actionSteps,
				"turn":                  0,
				"tool_calls":            toolCalls,
				"mode":                  mode,
				"attempt_delta_summary": summary,
				"attempt":               cfg.Attempt,
			})
		}
		_ = writeWorkerAssistContextEnvelope(cfg, task.TaskID, contextEnvelope, manager.Now)
	}()

	maxActionSteps := task.Budget.MaxSteps
	if maxActionSteps <= 0 {
		maxActionSteps = 6
	}
	maxToolCalls := task.Budget.MaxToolCalls
	if maxToolCalls <= 0 {
		maxToolCalls = 6
	}
	maxTurns := maxActionSteps * workerAssistTurnFactor
	if maxTurns < workerAssistMinTurns {
		maxTurns = workerAssistMinTurns
	}

	observations = make([]string, 0, workerAssistObsLimit)
	addObservation := func(entry string) {
		before := append([]string{}, observations...)
		observations = appendObservation(observations, entry)
		contextEnvelope.recordObservationAppend(before, observations)
	}
	lastActionKey := ""
	lastActionStreak := 0
	lastResultKey := ""
	lastResultStreak := 0
	recoverHint := ""
	loopBlocks := 0
	questionLoops := 0
	consecutiveToolTurns := 0
	consecutiveRecoverToolTurns := 0
	recoveryTransitions := 0
	missingToolInstallAttempts := map[string]struct{}{}
	missingToolRetryCount := map[string]int{}
	promptScope := buildAssistPromptScope(runScope, task.Targets)
	if loadedPriorEnvelope, priorErr := loadPreviousWorkerAssistContextEnvelope(cfg, task.TaskID); priorErr == nil && loadedPriorEnvelope != nil {
		priorEnvelope = loadedPriorEnvelope
		contextEnvelope.applyCarryover(priorEnvelope)
		carryoverObs := 0
		for _, entry := range priorEnvelope.Observations.RetainedTail {
			addObservation("carryover: " + strings.TrimSpace(entry))
			carryoverObs++
		}
		contextEnvelope.recordCarryoverObservations(carryoverObs)
		restoredLastFailure := false
		if strings.TrimSpace(priorEnvelope.Anchors.LastFailure) != "" {
			recoverHint = "prior attempt failure: " + strings.TrimSpace(priorEnvelope.Anchors.LastFailure)
			restoredLastFailure = true
		}
		restoredLastResultFingerprint := false
		if strings.TrimSpace(priorEnvelope.Anchors.LastResultFingerprint) != "" {
			lastResultKey = strings.TrimSpace(priorEnvelope.Anchors.LastResultFingerprint)
			restoredLastResultFingerprint = true
		}
		contextEnvelope.recordCarryoverRecoverySignals(restoredLastFailure, restoredLastResultFingerprint)
	}

	assistTurnMetadata := func(turnMeta workerAssistantTurnMeta, callTimeout time.Duration, remainingBudget time.Duration) map[string]any {
		model := strings.TrimSpace(turnMeta.Model)
		if model == "" {
			model = assistantModel
		}
		payload := map[string]any{
			"assist_mode":               assistMode,
			"assistant_model":           model,
			"llm_timeout_seconds":       int(callTimeout.Seconds()),
			"remaining_budget_seconds":  int(remainingBudget.Seconds()),
			"llm_trace_enabled":         turnMeta.TraceEnabled,
			"parse_repair_used":         turnMeta.ParseRepairUsed,
			"fallback_used":             turnMeta.FallbackUsed,
			"fallback_reason":           strings.TrimSpace(turnMeta.FallbackReason),
			"recovery_transition_count": recoveryTransitions,
		}
		if payload["fallback_reason"] == "" {
			delete(payload, "fallback_reason")
		}
		return payload
	}

	markRecoverTransition := func(turn int, cause string, extra map[string]any) error {
		recoveryTransitions++
		if recoveryTransitions < workerAssistMaxRecoveries {
			return nil
		}
		failErr := fmt.Errorf("assistant recovery exceeded limit (%d)", workerAssistMaxRecoveries)
		details := map[string]any{
			"step":                 actionSteps,
			"turn":                 turn,
			"tool_calls":           toolCalls,
			"mode":                 mode,
			"recovery_cause":       strings.TrimSpace(cause),
			"recovery_transitions": recoveryTransitions,
		}
		for k, v := range extra {
			details[k] = v
		}
		_ = emitWorkerFailure(manager, cfg, task, failErr, WorkerFailureAssistLoopDetected, details)
		return failErr
	}

	isSummaryTask := isAssistSummaryTask(task)
	isAdaptiveReplanTask := isAssistAdaptiveReplanTask(task)

	for turn := 1; turn <= maxTurns; turn++ {
		if isSummaryTask && mode == "recover" && recoveryTransitions > 0 && actionSteps >= workerAssistSummaryRecoverStepCap {
			if completionErr := emitAssistSummaryFallback(manager, cfg, task, assistantModel, mode, actionSteps, turn, toolCalls, fmt.Sprintf("recover step cap reached (%d)", actionSteps)); completionErr != nil {
				return completionErr
			}
			return nil
		}
		if actionSteps >= maxActionSteps {
			if isAdaptiveReplanTask && actionSteps > 0 {
				if completionErr := emitAssistAdaptiveBudgetFallback(manager, cfg, task, assistantModel, mode, lastActionKey, actionSteps, turn, toolCalls, fmt.Sprintf("action step cap reached (%d/%d)", actionSteps, maxActionSteps)); completionErr != nil {
					return completionErr
				}
				return nil
			}
			err := fmt.Errorf("assist budget exhausted: action_steps=%d max=%d", actionSteps, maxActionSteps)
			_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistExhausted, map[string]any{
				"step":         actionSteps,
				"turn":         turn,
				"tool_calls":   toolCalls,
				"action_steps": actionSteps,
			})
			return err
		}
		if toolCalls >= maxToolCalls {
			if isAdaptiveReplanTask && toolCalls > 0 {
				if completionErr := emitAssistAdaptiveBudgetFallback(manager, cfg, task, assistantModel, mode, lastActionKey, actionSteps, turn, toolCalls, fmt.Sprintf("tool call cap reached (%d/%d)", toolCalls, maxToolCalls)); completionErr != nil {
					return completionErr
				}
				return nil
			}
			err := fmt.Errorf("assist budget exhausted: tool_calls=%d max=%d", toolCalls, maxToolCalls)
			_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistExhausted, map[string]any{
				"step":         actionSteps,
				"turn":         turn,
				"tool_calls":   toolCalls,
				"action_steps": actionSteps,
			})
			return err
		}

		layeredContext := buildWorkerAssistLayeredContext(cfg, observations, recoverHint)
		recent := layeredContext.RecentLog
		chatHistory := layeredContext.ChatHistory
		if recent == "" {
			recent = strings.Join(observations, "\n\n")
			if recoverHint != "" {
				if recent != "" {
					recent += "\n\n"
				}
				recent += "Recovery context: " + recoverHint
			}
		}
		if chatHistory == "" {
			chatHistory = strings.Join(observations, "\n\n")
		}
		contextEnvelope.recordProgress(turn, actionSteps, toolCalls, len(observations))
		contextEnvelope.recordPrompt(mode, recoverHint, recent, chatHistory)
		suggestCtx, suggestCancel, callTimeout, remainingBudget, timeoutErr := newAssistCallContext(ctx)
		if timeoutErr != nil {
			_ = emitWorkerFailure(manager, cfg, task, timeoutErr, WorkerFailureAssistTimeout, map[string]any{
				"step":                     actionSteps,
				"turn":                     turn,
				"tool_calls":               toolCalls,
				"mode":                     mode,
				"remaining_budget_seconds": int(remainingBudget.Seconds()),
			})
			suggestCancel()
			return timeoutErr
		}
		suggestion, turnMeta, suggestErr := assistant.Suggest(suggestCtx, assist.Input{
			SessionID:   cfg.RunID,
			Scope:       promptScope,
			Targets:     task.Targets,
			Goal:        goal,
			Summary:     layeredContext.Summary,
			KnownFacts:  layeredContext.KnownFacts,
			Focus:       task.Goal,
			Plan:        strings.Join(task.DoneWhen, "; "),
			Inventory:   layeredContext.Inventory,
			WorkingDir:  workDir,
			RecentLog:   recent,
			ChatHistory: chatHistory,
			Mode:        mode,
		})
		suggestCancel()
		llmTracePath := ""
		if turnMeta.TraceEnabled {
			tracePath, traceErr := writeWorkerAssistLLMTrace(cfg, turn, mode, turnMeta, suggestion, suggestErr)
			if traceErr != nil {
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    "failed to write llm response trace artifact",
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
					"error":      traceErr.Error(),
				})
			} else if strings.TrimSpace(tracePath) != "" {
				llmTracePath = tracePath
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskArtifact, map[string]any{
					"type":      "assist_llm_response",
					"title":     fmt.Sprintf("assistant raw response turn %d (%s)", turn, cfg.TaskID),
					"path":      tracePath,
					"step":      actionSteps,
					"tool_call": toolCalls,
					"turn":      turn,
				})
			}
		}
		if suggestErr != nil {
			failureReason := WorkerFailureAssistUnavailable
			if isAssistTimeoutError(suggestErr, suggestCtx.Err(), ctx.Err()) {
				failureReason = WorkerFailureAssistTimeout
			} else {
				var parseErr assist.SuggestionParseError
				if errors.As(suggestErr, &parseErr) {
					failureReason = WorkerFailureAssistParseFailure
				}
			}
			details := map[string]any{
				"step":                     actionSteps,
				"turn":                     turn,
				"tool_calls":               toolCalls,
				"mode":                     mode,
				"llm_timeout_seconds":      int(callTimeout.Seconds()),
				"remaining_budget_seconds": int(remainingBudget.Seconds()),
			}
			for k, v := range assistTurnMetadata(turnMeta, callTimeout, remainingBudget) {
				details[k] = v
			}
			if llmTracePath != "" {
				details["llm_response_path"] = llmTracePath
			}
			_ = emitWorkerFailure(manager, cfg, task, suggestErr, failureReason, details)
			return suggestErr
		}
		if schemaErr := validateAssistSuggestionSchema(suggestion); schemaErr != nil {
			questionLoops = 0
			if mode == "recover" {
				loopBlocks++
				recoverHint = schemaErr.Error()
				addObservation("recovery: " + schemaErr.Error())
				if loopBlocks >= workerAssistMaxLoopBlocks {
					_ = emitWorkerFailure(manager, cfg, task, schemaErr, WorkerFailureAssistLoopDetected, map[string]any{
						"step":         actionSteps,
						"turn":         turn,
						"tool_calls":   toolCalls,
						"mode":         mode,
						"streak":       loopBlocks,
						"schema_error": schemaErr.Error(),
					})
					return schemaErr
				}
				continue
			}
			mode = "recover"
			recoverHint = schemaErr.Error()
			addObservation("recovery: " + schemaErr.Error())
			if recErr := markRecoverTransition(turn, "invalid_suggestion_schema", map[string]any{
				"schema_error": schemaErr.Error(),
			}); recErr != nil {
				return recErr
			}
			continue
		}

		progressMsg := strings.TrimSpace(suggestion.Summary)
		if progressMsg == "" {
			progressMsg = fmt.Sprintf("assistant returned %s", suggestion.Type)
		}
		progressPayload := map[string]any{
			"message":    progressMsg,
			"step":       actionSteps,
			"turn":       turn,
			"tool_calls": toolCalls,
			"mode":       mode,
			"type":       suggestion.Type,
		}
		for k, v := range assistTurnMetadata(turnMeta, callTimeout, remainingBudget) {
			progressPayload[k] = v
		}
		if llmTracePath != "" {
			progressPayload["llm_response_path"] = llmTracePath
		}
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, progressPayload)

		switch suggestion.Type {
		case "complete":
			consecutiveToolTurns = 0
			consecutiveRecoverToolTurns = 0
			final := strings.TrimSpace(suggestion.Final)
			if final == "" {
				final = strings.TrimSpace(suggestion.Summary)
			}
			return emitAssistCompletion(manager, cfg, task, assistantModel, "assist_complete", final, actionSteps, turn, toolCalls, nil)
		case "question":
			consecutiveToolTurns = 0
			consecutiveRecoverToolTurns = 0
			questionLoops++
			question := strings.TrimSpace(suggestion.Question)
			addObservation("assistant asked for input in non-interactive worker mode: " + question)
			autoAnswer := buildAutonomousQuestionAnswer(task, question, observations)
			if autoAnswer != "" {
				addObservation("autonomous answer: " + autoAnswer)
			}
			_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":             "assistant asked a question; worker is non-interactive, forcing autonomous recovery",
				"step":                actionSteps,
				"turn":                turn,
				"tool_calls":          toolCalls,
				"mode":                mode,
				"question":            question,
				"autonomous_answer":   autoAnswer,
				"question_loop_count": questionLoops,
			})
			if mode == "recover" {
				if isSummaryTask && questionLoops >= workerAssistSummaryRecoverQuestionCap {
					if completionErr := emitAssistSummaryFallback(manager, cfg, task, assistantModel, mode, actionSteps, turn, toolCalls, "repeated recover questions in non-interactive summary task"); completionErr != nil {
						return completionErr
					}
					return nil
				}
				if toolCalls >= workerAssistNoNewEvidenceToolCallCap && recoveryTransitions > 0 && actionSteps > 0 {
					if completionErr := emitAssistNoNewEvidenceCompletion(manager, cfg, task, assistantModel, mode, actionSteps, turn, toolCalls, "assistant_question", []string{"recover_non_interactive"}, questionLoops, "recover question churn after tool-call cap"); completionErr != nil {
						return completionErr
					}
					return nil
				}
				loopBlocks++
				recoverHint = "No user interaction is available in worker mode. Use autonomous context from recent observations, then return a concrete command, tool, or complete."
				if loopBlocks >= workerAssistMaxLoopBlocks {
					err := fmt.Errorf("assistant recovery loop detected: repeated questions without actionable step after %d recover turns", loopBlocks)
					_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistLoopDetected, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"type":       suggestion.Type,
						"mode":       mode,
						"streak":     loopBlocks,
						"question":   question,
					})
					return err
				}
				continue
			}
			if questionLoops >= 3 {
				err := fmt.Errorf("assistant requested user input in non-interactive worker mode: %s", question)
				_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistNeedsInput, map[string]any{
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"question":   question,
				})
				return err
			}
			mode = "recover"
			loopBlocks++
			recoverHint = "No user interaction is available in worker mode. Use autonomous context from recent observations, then return a concrete command, tool, or complete."
			if recErr := markRecoverTransition(turn, "question_without_interaction", map[string]any{
				"question": question,
			}); recErr != nil {
				return recErr
			}
			continue
		case "plan", "noop":
			consecutiveToolTurns = 0
			consecutiveRecoverToolTurns = 0
			questionLoops = 0
			if mode == "recover" {
				if isSummaryTask && loopBlocks+1 >= workerAssistSummaryRecoverLoopCap {
					if completionErr := emitAssistSummaryFallback(manager, cfg, task, assistantModel, mode, actionSteps, turn, toolCalls, fmt.Sprintf("recover %s churn in summary task", suggestion.Type)); completionErr != nil {
						return completionErr
					}
					return nil
				}
				if toolCalls >= workerAssistNoNewEvidenceToolCallCap && recoveryTransitions > 0 && actionSteps > 0 {
					if completionErr := emitAssistNoNewEvidenceCompletion(manager, cfg, task, assistantModel, mode, actionSteps, turn, toolCalls, "assistant_"+suggestion.Type, []string{"recover_non_action"}, 1, "recover non-action churn after tool-call cap"); completionErr != nil {
						return completionErr
					}
					return nil
				}
				loopBlocks++
				recoverHint = fmt.Sprintf("assistant returned %s in recover mode without an executable step; return one concrete command or tool action", suggestion.Type)
				addObservation("recovery: " + recoverHint)
				if recErr := markRecoverTransition(turn, "recover_non_action", map[string]any{
					"type": suggestion.Type,
				}); recErr != nil {
					return recErr
				}
				if loopBlocks >= workerAssistMaxLoopBlocks {
					err := fmt.Errorf("assistant recovery loop detected: no actionable step after %d recover turns", loopBlocks)
					_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistLoopDetected, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"type":       suggestion.Type,
						"mode":       mode,
						"streak":     loopBlocks,
					})
					return err
				}
				continue
			}
			mode = "execute-step"
			addObservation(summarizeSuggestion(suggestion))
			continue
		case "tool":
			consecutiveToolTurns++
			if mode == "recover" {
				consecutiveRecoverToolTurns++
			} else {
				consecutiveRecoverToolTurns = 0
			}
			if suggestion.Tool == nil {
				questionLoops = 0
				if mode == "recover" {
					addObservation("recovery: assistant returned tool without spec")
					recoverHint = "Tool suggestion was invalid (missing spec). Return a concrete command or valid tool with files and run command."
					loopBlocks++
					if recErr := markRecoverTransition(turn, "tool_missing_spec", nil); recErr != nil {
						return recErr
					}
					if loopBlocks >= workerAssistMaxLoopBlocks {
						err := fmt.Errorf("assistant tool suggestion missing spec")
						_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistNoAction, map[string]any{
							"step":       actionSteps,
							"turn":       turn,
							"tool_calls": toolCalls,
						})
						return err
					}
					continue
				}
				err := fmt.Errorf("assistant tool suggestion missing spec")
				mode = "recover"
				recoverHint = err.Error()
				addObservation("recovery: " + err.Error())
				if recErr := markRecoverTransition(turn, "tool_missing_spec", nil); recErr != nil {
					return recErr
				}
				continue
			}
			runCommand := strings.TrimSpace(suggestion.Tool.Run.Command)
			runArgs := append([]string{}, suggestion.Tool.Run.Args...)
			if mode == "recover" && actionSteps > 0 {
				pivotBasis, pivotSource, pivotKind := resolveRecoverPivotBasis(strings.TrimSpace(suggestion.Summary), recoverHint, observations)
				if pivotBasis == "" {
					return emitAssistNoProgressFailure(manager, cfg, task, mode, actionSteps, turn, toolCalls, runCommand, runArgs, "recover pivot missing evidence anchor or unknown-under-test citation", map[string]any{
						"recover_pivot_contract": "missing_basis",
					})
				}
				addObservation(fmt.Sprintf("recover pivot basis (%s/%s): %s", pivotKind, pivotSource, pivotBasis))
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":            "recover pivot accepted with citation",
					"step":               actionSteps,
					"turn":               turn,
					"tool_calls":         toolCalls,
					"mode":               mode,
					"pivot_basis":        pivotBasis,
					"pivot_basis_source": pivotSource,
					"pivot_basis_kind":   pivotKind,
				})
			}
			if consecutiveRecoverToolTurns > workerAssistMaxConsecutiveRecoverToolTurns {
				loopBlocks++
				recoverHint = "recover tool-churn detected; stop generating helper scripts and return a direct command or complete"
				addObservation(fmt.Sprintf("recovery: recover tool churn blocked for %s", strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))))
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":                        "recover tool churn detected; requesting direct command or completion",
					"step":                           actionSteps,
					"turn":                           turn,
					"tool_calls":                     toolCalls,
					"mode":                           mode,
					"consecutive_tool_turns":         consecutiveToolTurns,
					"consecutive_recover_tool_turns": consecutiveRecoverToolTurns,
				})
				if loopBlocks >= workerAssistMaxLoopBlocks {
					currentKey := buildAssistActionKey(runCommand, runArgs)
					if actionSteps >= workerAssistNoNewEvidenceToolCallCap && recoveryTransitions > 0 && isNoNewEvidenceCandidate(runCommand, runArgs) {
						heldAction := strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))
						if heldAction == "" {
							heldAction = "unknown"
						}
						_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
							"message":                        "no new evidence after extended recover tool churn; completing task with bounded fallback",
							"step":                           actionSteps,
							"turn":                           turn,
							"tool_calls":                     toolCalls,
							"mode":                           mode,
							"consecutive_tool_turns":         consecutiveToolTurns,
							"consecutive_recover_tool_turns": consecutiveRecoverToolTurns,
							"bounded_fallback":               "no_new_evidence",
							"last_repeated_action":           heldAction,
						})
						return emitAssistNoNewEvidenceCompletion(
							manager,
							cfg,
							task,
							assistantModel,
							mode,
							actionSteps,
							turn,
							toolCalls,
							runCommand,
							runArgs,
							loopBlocks,
							"extended_recover_tool_churn",
						)
					}
					if actionSteps > 0 && currentKey != "" && currentKey == lastActionKey && lastActionStreak >= workerAssistLoopMaxRepeat {
						heldAction := strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))
						if heldAction == "" {
							heldAction = "unknown"
						}
						_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
							"message":                        "no new evidence after repeated recover tool churn; completing task with bounded fallback",
							"step":                           actionSteps,
							"turn":                           turn,
							"tool_calls":                     toolCalls,
							"mode":                           mode,
							"consecutive_tool_turns":         consecutiveToolTurns,
							"consecutive_recover_tool_turns": consecutiveRecoverToolTurns,
							"bounded_fallback":               "no_new_evidence",
							"last_repeated_action":           heldAction,
						})
						return emitAssistNoNewEvidenceCompletion(
							manager,
							cfg,
							task,
							assistantModel,
							mode,
							actionSteps,
							turn,
							toolCalls,
							runCommand,
							runArgs,
							consecutiveRecoverToolTurns,
							"repeated_recover_tool_churn",
						)
					}
					err := fmt.Errorf("assistant tool loop detected: consecutive_tool_turns=%d consecutive_recover_tool_turns=%d", consecutiveToolTurns, consecutiveRecoverToolTurns)
					_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistLoopDetected, map[string]any{
						"step":                           actionSteps,
						"turn":                           turn,
						"tool_calls":                     toolCalls,
						"command":                        runCommand,
						"args":                           runArgs,
						"consecutive_tool_turns":         consecutiveToolTurns,
						"consecutive_recover_tool_turns": consecutiveRecoverToolTurns,
					})
					return err
				}
				continue
			}
			if consecutiveToolTurns > workerAssistMaxConsecutiveToolTurns && mode != "recover" {
				loopBlocks++
				mode = "recover"
				recoverHint = "tool-churn detected; avoid creating another helper script and respond with a concrete command or complete"
				addObservation(fmt.Sprintf("recovery: tool churn blocked for %s", strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))))
				if recErr := markRecoverTransition(turn, "tool_churn_execute", map[string]any{
					"command":                        runCommand,
					"args":                           runArgs,
					"consecutive_tool_turns":         consecutiveToolTurns,
					"consecutive_recover_tool_turns": consecutiveRecoverToolTurns,
				}); recErr != nil {
					return recErr
				}
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":                        "tool churn detected; requesting direct command or completion before continuing",
					"step":                           actionSteps,
					"turn":                           turn,
					"tool_calls":                     toolCalls,
					"mode":                           mode,
					"consecutive_tool_turns":         consecutiveToolTurns,
					"consecutive_recover_tool_turns": consecutiveRecoverToolTurns,
				})
				if loopBlocks >= workerAssistMaxLoopBlocks {
					err := fmt.Errorf("assistant tool loop detected: consecutive_tool_turns=%d consecutive_recover_tool_turns=%d", consecutiveToolTurns, consecutiveRecoverToolTurns)
					_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistLoopDetected, map[string]any{
						"step":                           actionSteps,
						"turn":                           turn,
						"tool_calls":                     toolCalls,
						"command":                        runCommand,
						"args":                           runArgs,
						"consecutive_tool_turns":         consecutiveToolTurns,
						"consecutive_recover_tool_turns": consecutiveRecoverToolTurns,
					})
					return err
				}
				continue
			}
			actionKey := buildAssistActionKey(runCommand, runArgs)
			lastActionKey, lastActionStreak = trackAssistActionStreak(lastActionKey, lastActionStreak, actionKey)
			contextEnvelope.recordActionFingerprint(actionKey, lastActionStreak)
			if lastActionStreak > workerAssistLoopMaxRepeat {
				wasRecover := mode == "recover"
				loopBlocks++
				mode = "recover"
				recoverHint = "same action repeated too many times; choose a different command path"
				addObservation(fmt.Sprintf("recovery: repeated action blocked for %s", strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))))
				if !wasRecover {
					if recErr := markRecoverTransition(turn, "repeated_action", map[string]any{
						"command": runCommand,
						"args":    runArgs,
						"streak":  lastActionStreak,
					}); recErr != nil {
						return recErr
					}
				}
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    "same action repeated too many times; waiting for alternative command",
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
					"streak":     lastActionStreak,
				})
				if loopBlocks >= workerAssistMaxLoopBlocks {
					if wasRecover && actionSteps > 0 {
						heldAction := strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))
						if heldAction == "" {
							heldAction = "unknown"
						}
						_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
							"message":              "no new evidence after repeated recover attempts; completing task with bounded fallback",
							"step":                 actionSteps,
							"turn":                 turn,
							"tool_calls":           toolCalls,
							"mode":                 mode,
							"streak":               lastActionStreak,
							"bounded_fallback":     "no_new_evidence",
							"last_repeated_action": heldAction,
						})
						return emitAssistNoNewEvidenceCompletion(
							manager,
							cfg,
							task,
							assistantModel,
							mode,
							actionSteps,
							turn,
							toolCalls,
							runCommand,
							runArgs,
							lastActionStreak,
							"repeated_recover_attempts",
						)
					}
					err := fmt.Errorf("repeated command blocked too many times")
					_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistLoopDetected, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"command":    runCommand,
						"args":       runArgs,
						"streak":     lastActionStreak,
					})
					return err
				}
				continue
			}
			actionSteps++
			toolCalls++
			loopBlocks = 0
			questionLoops = 0
			wasRecover := mode == "recover"
			result, runErr := executeWorkerAssistTool(ctx, cfg, task, scopePolicy, workDir, suggestion.Tool)
			logPath, logErr := writeWorkerActionLog(cfg, fmt.Sprintf("%s-a%d-s%d-t%d.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt, actionSteps, toolCalls), result.output)
			if logErr != nil {
				_ = emitWorkerFailure(manager, cfg, task, logErr, WorkerFailureArtifactWrite, nil)
				return logErr
			}
			_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskArtifact, map[string]any{
				"type":      "command_log",
				"title":     fmt.Sprintf("assist tool output (%s)", cfg.TaskID),
				"path":      logPath,
				"command":   result.command,
				"args":      result.args,
				"step":      actionSteps,
				"turn":      turn,
				"tool_call": toolCalls,
				"exit_code": commandExitCode(runErr),
			})
			summary, summaryMeta := summarizeCommandResultWithMeta(result.command, result.args, runErr, result.output)
			contextEnvelope.recordCommandSummary(summaryMeta)
			addObservation(summary)
			var completed bool
			var completionErr error
			lastResultKey, lastResultStreak, completed, completionErr = updateAssistResultStreakAndCheckNoEvidence(
				manager,
				cfg,
				task,
				contextEnvelope,
				assistantModel,
				mode,
				actionSteps,
				turn,
				toolCalls,
				recoveryTransitions,
				result.command,
				result.args,
				runErr,
				result.output,
				lastResultKey,
				lastResultStreak,
			)
			if completionErr != nil {
				return completionErr
			}
			if completed {
				return nil
			}
			contextEnvelope.recordCommandOutcome(result.command, result.args, runErr, lastResultKey)
			if runErr != nil {
				if isAssistInvalidToolSpecError(runErr) {
					mode = "recover"
					recoverHint = summary
					addObservation("recovery: invalid tool specification; request command/tool with concrete files+run")
					if recErr := markRecoverTransition(turn, "invalid_tool_spec", map[string]any{
						"command": result.command,
						"args":    result.args,
					}); recErr != nil {
						return recErr
					}
					continue
				}
				failureHandled, failureErr := handleAssistExecutionFailure(
					ctx,
					manager,
					cfg,
					task,
					workDir,
					actionSteps,
					turn,
					toolCalls,
					result.command,
					result.args,
					runErr,
					result.output,
					summary,
					logPath,
					&mode,
					&recoverHint,
					missingToolInstallAttempts,
					missingToolRetryCount,
					addObservation,
					markRecoverTransition,
					"tool_execution_failed",
					true,
				)
				if failureErr != nil {
					return failureErr
				}
				if failureHandled {
					continue
				}
			}
			settleAssistModeAfterSuccessfulExecution(&mode, &recoverHint, &recoveryTransitions, wasRecover)
		case "command":
			consecutiveToolTurns = 0
			consecutiveRecoverToolTurns = 0
			command := strings.TrimSpace(suggestion.Command)
			args := append([]string{}, suggestion.Args...)
			if command == "" {
				err := fmt.Errorf("assistant returned empty command")
				_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistNoAction, map[string]any{
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
				})
				return err
			}
			if !cfg.Diagnostic {
				command, args = normalizeWorkerAssistCommand(command, args)
			}
			args, scriptPathNote, scriptPathAdapted := resolveShellToolScriptPath(workDir, command, args)
			if scriptPathAdapted {
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    scriptPathNote,
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
					"command":    command,
					"args":       args,
				})
			}
			if !cfg.Diagnostic {
				prepared := prepareRuntimeCommand(scopePolicy, task, command, args)
				command = prepared.Command
				args = prepared.Args
				emitRuntimePreparationProgress(
					manager,
					cfg.RunID,
					WorkerSignalID(cfg.WorkerID),
					cfg.TaskID,
					actionSteps,
					turn,
					toolCalls,
					"tool_calls",
					mode,
					command,
					args,
					runtimePreparationMessages(prepared),
				)
			}
			if !isWorkerLocalBuiltin(command) {
				if err := scopePolicy.ValidateCommandTargets(command, args); err != nil {
					_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureScopeDenied, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"command":    command,
						"args":       args,
					})
					return err
				}
			}
			if blocked, reason := shouldSkipMissingToolInstall(command, command, args); blocked {
				loopBlocks++
				recoverHint = fmt.Sprintf("%s; use available tools (%s) or forge a helper via type=tool", reason, discoverAvailableFallbackTools())
				addObservation("recovery: " + recoverHint)
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    "blocked non-executable workflow command; requesting executable action",
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       "recover",
					"command":    command,
					"args":       args,
					"reason":     reason,
				})
				if mode != "recover" {
					mode = "recover"
					if recErr := markRecoverTransition(turn, "pseudo_workflow_command", map[string]any{
						"command": command,
						"args":    args,
						"reason":  reason,
					}); recErr != nil {
						return recErr
					}
				}
				if loopBlocks >= workerAssistMaxLoopBlocks {
					return emitAssistNoProgressFailure(manager, cfg, task, mode, actionSteps, turn, toolCalls, command, args, "repeated non-executable workflow command suggestions", map[string]any{
						"pseudo_command_reason": reason,
					})
				}
				continue
			}
			if mode == "recover" && actionSteps > 0 {
				pivotBasis, pivotSource, pivotKind := resolveRecoverPivotBasis(strings.TrimSpace(suggestion.Summary), recoverHint, observations)
				if pivotBasis == "" {
					return emitAssistNoProgressFailure(manager, cfg, task, mode, actionSteps, turn, toolCalls, command, args, "recover pivot missing evidence anchor or unknown-under-test citation", map[string]any{
						"recover_pivot_contract": "missing_basis",
					})
				}
				addObservation(fmt.Sprintf("recover pivot basis (%s/%s): %s", pivotKind, pivotSource, pivotBasis))
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":            "recover pivot accepted with citation",
					"step":               actionSteps,
					"turn":               turn,
					"tool_calls":         toolCalls,
					"mode":               mode,
					"pivot_basis":        pivotBasis,
					"pivot_basis_source": pivotSource,
					"pivot_basis_kind":   pivotKind,
				})
			}
			key := buildAssistActionKey(command, args)
			lastActionKey, lastActionStreak = trackAssistActionStreak(lastActionKey, lastActionStreak, key)
			contextEnvelope.recordActionFingerprint(key, lastActionStreak)
			if isLowValueListingCommand(command, args) && lastActionStreak >= workerAssistNoNewEvidenceResultRepeat {
				return emitAssistNoProgressFailure(manager, cfg, task, mode, actionSteps, turn, toolCalls, command, args, "repeated low-value listing command streak", map[string]any{
					"streak": lastActionStreak,
				})
			}
			if mode == "recover" && lastActionStreak >= workerAssistNoNewEvidenceResultRepeat && isNoNewEvidenceCandidate(command, args) {
				if completionErr := emitAssistNoNewEvidenceCompletion(manager, cfg, task, assistantModel, mode, actionSteps, turn, toolCalls, command, args, lastActionStreak, "repeated recover command streak without new evidence"); completionErr != nil {
					return completionErr
				}
				return nil
			}
			if lastActionStreak > workerAssistLoopMaxRepeat {
				wasRecover := mode == "recover"
				loopBlocks++
				mode = "recover"
				recoverHint = "same command repeated too many times; choose a different command path"
				addObservation(fmt.Sprintf("recovery: repeated command blocked for %s", strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))))
				if !wasRecover {
					if recErr := markRecoverTransition(turn, "repeated_command", map[string]any{
						"command": command,
						"args":    args,
						"streak":  lastActionStreak,
					}); recErr != nil {
						return recErr
					}
				}
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    "same command repeated too many times; waiting for alternative",
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
					"streak":     lastActionStreak,
				})
				if loopBlocks >= workerAssistMaxLoopBlocks {
					err := fmt.Errorf("repeated command blocked too many times")
					_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistLoopDetected, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"command":    command,
						"args":       args,
						"streak":     lastActionStreak,
					})
					return err
				}
				continue
			}
			wasRecover := mode == "recover"
			actionSteps++
			toolCalls++
			loopBlocks = 0
			questionLoops = 0
			result := executeWorkerAssistCommand(ctx, cfg, task, command, args, workDir)
			logPath, logErr := writeWorkerActionLog(cfg, fmt.Sprintf("%s-a%d-s%d-t%d.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt, actionSteps, toolCalls), result.output)
			if logErr != nil {
				_ = emitWorkerFailure(manager, cfg, task, logErr, WorkerFailureArtifactWrite, nil)
				return logErr
			}
			_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskArtifact, map[string]any{
				"type":      "command_log",
				"title":     fmt.Sprintf("assist command output (%s)", cfg.TaskID),
				"path":      logPath,
				"command":   result.command,
				"args":      result.args,
				"step":      actionSteps,
				"turn":      turn,
				"tool_call": toolCalls,
				"exit_code": commandExitCode(result.runErr),
			})
			summary, summaryMeta := summarizeCommandResultWithMeta(result.command, result.args, result.runErr, result.output)
			contextEnvelope.recordCommandSummary(summaryMeta)
			addObservation(summary)
			var completed bool
			var completionErr error
			lastResultKey, lastResultStreak, completed, completionErr = updateAssistResultStreakAndCheckNoEvidence(
				manager,
				cfg,
				task,
				contextEnvelope,
				assistantModel,
				mode,
				actionSteps,
				turn,
				toolCalls,
				recoveryTransitions,
				result.command,
				result.args,
				result.runErr,
				result.output,
				lastResultKey,
				lastResultStreak,
			)
			if completionErr != nil {
				return completionErr
			}
			if completed {
				return nil
			}
			contextEnvelope.recordCommandOutcome(result.command, result.args, result.runErr, lastResultKey)
			if result.runErr != nil {
				failureHandled, failureErr := handleAssistExecutionFailure(
					ctx,
					manager,
					cfg,
					task,
					workDir,
					actionSteps,
					turn,
					toolCalls,
					result.command,
					result.args,
					result.runErr,
					result.output,
					summary,
					logPath,
					&mode,
					&recoverHint,
					missingToolInstallAttempts,
					missingToolRetryCount,
					addObservation,
					markRecoverTransition,
					"command_execution_failed",
					false,
				)
				if failureErr != nil {
					return failureErr
				}
				if failureHandled {
					continue
				}
			}
			settleAssistModeAfterSuccessfulExecution(&mode, &recoverHint, &recoveryTransitions, wasRecover)
		default:
			err := fmt.Errorf("assistant returned unsupported type %q", suggestion.Type)
			_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistNoAction, map[string]any{
				"step":       actionSteps,
				"turn":       turn,
				"tool_calls": toolCalls,
				"type":       suggestion.Type,
			})
			return err
		}
	}

	err = fmt.Errorf("assist exhausted max turns (%d) without completion", maxTurns)
	_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistExhausted, map[string]any{
		"step":         actionSteps,
		"turn":         maxTurns,
		"tool_calls":   toolCalls,
		"action_steps": actionSteps,
	})
	return err
}
