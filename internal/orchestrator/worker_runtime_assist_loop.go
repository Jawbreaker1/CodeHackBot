package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func runWorkerAssistTask(ctx context.Context, manager *Manager, cfg WorkerRunConfig, task TaskSpec, action TaskAction, scopePolicy *ScopePolicy, runScope Scope, workDir string) error {
	assistantModel, assistMode, assistant, err := buildWorkerAssistant()
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

	observations := make([]string, 0, workerAssistObsLimit)
	lastActionKey := ""
	lastActionStreak := 0
	lastResultKey := ""
	lastResultStreak := 0
	mode := "execute-step"
	recoverHint := ""
	toolCalls := 0
	actionSteps := 0
	loopBlocks := 0
	questionLoops := 0
	consecutiveToolTurns := 0
	consecutiveRecoverToolTurns := 0
	recoveryTransitions := 0
	promptScope := buildAssistPromptScope(runScope, task.Targets)

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

	emitAssistCompletion := func(statusReason, final string, step, turn, toolCalls int, extraMeta map[string]any) error {
		statusReason = strings.TrimSpace(statusReason)
		if statusReason == "" {
			statusReason = "assist_complete"
		}
		final = strings.TrimSpace(final)
		if final == "" {
			final = "assistant marked task complete"
		}
		logPath, logErr := writeWorkerActionLog(cfg, fmt.Sprintf("%s-a%d-complete.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt), []byte(final+"\n"))
		if logErr != nil {
			_ = emitWorkerFailure(manager, cfg, task, logErr, WorkerFailureArtifactWrite, nil)
			return logErr
		}
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskArtifact, map[string]any{
			"type":      "assist_summary",
			"title":     fmt.Sprintf("assistant completion summary (%s)", cfg.TaskID),
			"path":      logPath,
			"step":      step,
			"tool_call": toolCalls,
		})
		findingMeta := map[string]any{
			"attempt":       cfg.Attempt,
			"reason":        statusReason,
			"assistant":     assistantModel,
			"steps":         step,
			"turns":         turn,
			"tool_calls":    toolCalls,
			"final_summary": final,
		}
		for k, v := range extraMeta {
			findingMeta[k] = v
		}
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFinding, map[string]any{
			"target":       primaryTaskTarget(task),
			"finding_type": "task_execution_result",
			"title":        "task action completed",
			"state":        FindingStateVerified,
			"severity":     "info",
			"confidence":   "medium",
			"source":       "worker_runtime_assist",
			"evidence":     []string{logPath},
			"metadata":     findingMeta,
		})
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskCompleted, map[string]any{
			"attempt":   cfg.Attempt,
			"worker_id": cfg.WorkerID,
			"reason":    statusReason,
			"log_path":  logPath,
			"completion_contract": map[string]any{
				"status_reason":       statusReason,
				"required_artifacts":  task.ExpectedArtifacts,
				"produced_artifacts":  []string{logPath},
				"required_findings":   []string{"task_execution_result"},
				"produced_findings":   []string{"task_execution_result"},
				"verification_status": "reported_by_worker",
			},
		})
		return nil
	}

	emitNoNewEvidenceCompletion := func(turn int, command string, args []string, resultStreak int, reason string) error {
		lastAction := strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
		if lastAction == "" {
			lastAction = "unknown"
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "repeated identical command results"
		}
		summary := fmt.Sprintf("No new evidence after repeated identical runtime results. Last action: %s (result_streak=%d, reason=%s).", lastAction, resultStreak, reason)
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
			"message":              "no new evidence after repeated identical results; completing task with bounded fallback",
			"step":                 actionSteps,
			"turn":                 turn,
			"tool_calls":           toolCalls,
			"mode":                 mode,
			"result_streak":        resultStreak,
			"bounded_fallback":     "no_new_evidence",
			"last_repeated_action": lastAction,
			"fallback_reason":      reason,
		})
		return emitAssistCompletion("assist_no_new_evidence", summary, actionSteps, turn, toolCalls, map[string]any{
			"result_streak":        resultStreak,
			"bounded_fallback":     "no_new_evidence",
			"last_repeated_action": lastAction,
			"fallback_reason":      reason,
		})
	}
	isSummaryTask := isAssistSummaryTask(task)
	isAdaptiveReplanTask := isAssistAdaptiveReplanTask(task)
	emitSummaryFallback := func(turn int, fallbackReason string) error {
		fallbackReason = strings.TrimSpace(fallbackReason)
		if fallbackReason == "" {
			fallbackReason = "summary recover fallback"
		}
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
			"message":          "summary task recover fallback triggered; completing with available evidence",
			"step":             actionSteps,
			"turn":             turn,
			"tool_calls":       toolCalls,
			"mode":             mode,
			"bounded_fallback": "summary_autocomplete",
			"fallback_reason":  fallbackReason,
		})
		final := fmt.Sprintf("Completed summary synthesis from available run evidence with bounded recover fallback (%s).", fallbackReason)
		return emitAssistCompletion("assist_summary_autocomplete", final, actionSteps, turn, toolCalls, map[string]any{
			"bounded_fallback": "summary_autocomplete",
			"fallback_reason":  fallbackReason,
		})
	}
	emitAdaptiveBudgetFallback := func(turn int, fallbackReason string) error {
		fallbackReason = strings.TrimSpace(fallbackReason)
		if fallbackReason == "" {
			fallbackReason = "adaptive replan budget cap"
		}
		lastAction := strings.TrimSpace(lastActionKey)
		if lastAction == "" {
			lastAction = "unknown"
		}
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
			"message":              "adaptive replan budget cap reached; completing with bounded fallback",
			"step":                 actionSteps,
			"turn":                 turn,
			"tool_calls":           toolCalls,
			"mode":                 mode,
			"bounded_fallback":     "adaptive_replan_budget_cap",
			"last_repeated_action": lastAction,
			"fallback_reason":      fallbackReason,
		})
		final := fmt.Sprintf("Adaptive replan completed with bounded fallback after budget cap (%s). Last action: %s.", fallbackReason, lastAction)
		return emitAssistCompletion("assist_no_new_evidence", final, actionSteps, turn, toolCalls, map[string]any{
			"bounded_fallback":     "adaptive_replan_budget_cap",
			"last_repeated_action": lastAction,
			"fallback_reason":      fallbackReason,
		})
	}

	for turn := 1; turn <= maxTurns; turn++ {
		if isSummaryTask && mode == "recover" && recoveryTransitions > 0 && actionSteps >= workerAssistSummaryRecoverStepCap {
			if completionErr := emitSummaryFallback(turn, fmt.Sprintf("recover step cap reached (%d)", actionSteps)); completionErr != nil {
				return completionErr
			}
			return nil
		}
		if actionSteps >= maxActionSteps {
			if isAdaptiveReplanTask && actionSteps > 0 {
				if completionErr := emitAdaptiveBudgetFallback(turn, fmt.Sprintf("action step cap reached (%d/%d)", actionSteps, maxActionSteps)); completionErr != nil {
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
				if completionErr := emitAdaptiveBudgetFallback(turn, fmt.Sprintf("tool call cap reached (%d/%d)", toolCalls, maxToolCalls)); completionErr != nil {
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

		recent := strings.Join(observations, "\n\n")
		if recoverHint != "" {
			if recent != "" {
				recent += "\n\n"
			}
			recent += "Recovery context: " + recoverHint
		}
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
			Summary:     fmt.Sprintf("Task %s attempt %d (%s)", task.TaskID, cfg.Attempt, assistantModel),
			Focus:       task.Goal,
			Plan:        strings.Join(task.DoneWhen, "; "),
			WorkingDir:  workDir,
			RecentLog:   recent,
			ChatHistory: recent,
			Mode:        mode,
		})
		suggestCancel()
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
			_ = emitWorkerFailure(manager, cfg, task, suggestErr, failureReason, details)
			return suggestErr
		}
		if schemaErr := validateAssistSuggestionSchema(suggestion); schemaErr != nil {
			questionLoops = 0
			if mode == "recover" {
				loopBlocks++
				recoverHint = schemaErr.Error()
				observations = appendObservation(observations, "recovery: "+schemaErr.Error())
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
			observations = appendObservation(observations, "recovery: "+schemaErr.Error())
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
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, progressPayload)

		switch suggestion.Type {
		case "complete":
			consecutiveToolTurns = 0
			consecutiveRecoverToolTurns = 0
			final := strings.TrimSpace(suggestion.Final)
			if final == "" {
				final = strings.TrimSpace(suggestion.Summary)
			}
			return emitAssistCompletion("assist_complete", final, actionSteps, turn, toolCalls, nil)
		case "question":
			consecutiveToolTurns = 0
			consecutiveRecoverToolTurns = 0
			questionLoops++
			question := strings.TrimSpace(suggestion.Question)
			observations = appendObservation(observations, "assistant asked for input in non-interactive worker mode: "+question)
			autoAnswer := buildAutonomousQuestionAnswer(task, question, observations)
			if autoAnswer != "" {
				observations = appendObservation(observations, "autonomous answer: "+autoAnswer)
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
					if completionErr := emitSummaryFallback(turn, "repeated recover questions in non-interactive summary task"); completionErr != nil {
						return completionErr
					}
					return nil
				}
				if toolCalls >= workerAssistNoNewEvidenceToolCallCap && recoveryTransitions > 0 && actionSteps > 0 {
					if completionErr := emitNoNewEvidenceCompletion(turn, "assistant_question", []string{"recover_non_interactive"}, questionLoops, "recover question churn after tool-call cap"); completionErr != nil {
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
					if completionErr := emitSummaryFallback(turn, fmt.Sprintf("recover %s churn in summary task", suggestion.Type)); completionErr != nil {
						return completionErr
					}
					return nil
				}
				if toolCalls >= workerAssistNoNewEvidenceToolCallCap && recoveryTransitions > 0 && actionSteps > 0 {
					if completionErr := emitNoNewEvidenceCompletion(turn, "assistant_"+suggestion.Type, []string{"recover_non_action"}, 1, "recover non-action churn after tool-call cap"); completionErr != nil {
						return completionErr
					}
					return nil
				}
				loopBlocks++
				recoverHint = fmt.Sprintf("assistant returned %s in recover mode without an executable step; return one concrete command or tool action", suggestion.Type)
				observations = appendObservation(observations, "recovery: "+recoverHint)
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
			observations = appendObservation(observations, summarizeSuggestion(suggestion))
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
					observations = appendObservation(observations, "recovery: assistant returned tool without spec")
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
				observations = appendObservation(observations, "recovery: "+err.Error())
				if recErr := markRecoverTransition(turn, "tool_missing_spec", nil); recErr != nil {
					return recErr
				}
				continue
			}
			runCommand := strings.TrimSpace(suggestion.Tool.Run.Command)
			runArgs := append([]string{}, suggestion.Tool.Run.Args...)
			if consecutiveRecoverToolTurns > workerAssistMaxConsecutiveRecoverToolTurns {
				loopBlocks++
				recoverHint = "recover tool-churn detected; stop generating helper scripts and return a direct command or complete"
				observations = appendObservation(observations, fmt.Sprintf("recovery: recover tool churn blocked for %s", strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))))
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
						summary := fmt.Sprintf("No new evidence after extended recover tool churn. Last repeated action: %s (action_steps=%d, blocked_turns=%d).", heldAction, actionSteps, loopBlocks)
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
						return emitAssistCompletion("assist_no_new_evidence", summary, actionSteps, turn, toolCalls, map[string]any{
							"loop_blocks":                    loopBlocks,
							"consecutive_tool_turns":         consecutiveToolTurns,
							"consecutive_recover_tool_turns": consecutiveRecoverToolTurns,
							"bounded_fallback":               "no_new_evidence",
							"last_repeated_action":           heldAction,
							"fallback_reason":                "extended_recover_tool_churn",
						})
					}
					if actionSteps > 0 && currentKey != "" && currentKey == lastActionKey && lastActionStreak >= workerAssistLoopMaxRepeat {
						heldAction := strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))
						if heldAction == "" {
							heldAction = "unknown"
						}
						summary := fmt.Sprintf("No new evidence after repeated recover tool churn. Last repeated action: %s (consecutive_recover_tool_turns=%d, blocked_turns=%d).", heldAction, consecutiveRecoverToolTurns, loopBlocks)
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
						return emitAssistCompletion("assist_no_new_evidence", summary, actionSteps, turn, toolCalls, map[string]any{
							"loop_blocks":                    loopBlocks,
							"consecutive_tool_turns":         consecutiveToolTurns,
							"consecutive_recover_tool_turns": consecutiveRecoverToolTurns,
							"bounded_fallback":               "no_new_evidence",
							"last_repeated_action":           heldAction,
						})
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
				observations = appendObservation(observations, fmt.Sprintf("recovery: tool churn blocked for %s", strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))))
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
			if lastActionStreak > workerAssistLoopMaxRepeat {
				wasRecover := mode == "recover"
				loopBlocks++
				mode = "recover"
				recoverHint = "same action repeated too many times; choose a different command path"
				observations = appendObservation(observations, fmt.Sprintf("recovery: repeated action blocked for %s", strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))))
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
						summary := fmt.Sprintf("No new evidence after repeated recover attempts. Last repeated action: %s (streak=%d, blocked_turns=%d).", heldAction, lastActionStreak, loopBlocks)
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
						return emitAssistCompletion("assist_no_new_evidence", summary, actionSteps, turn, toolCalls, map[string]any{
							"loop_blocks":          loopBlocks,
							"streak":               lastActionStreak,
							"bounded_fallback":     "no_new_evidence",
							"last_repeated_action": heldAction,
						})
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
			observations = appendObservation(observations, summarizeCommandResult(result.command, result.args, runErr, result.output))
			if runErr != nil {
				lastResultKey = ""
				lastResultStreak = 0
			} else {
				lastResultKey, lastResultStreak = trackAssistResultStreak(lastResultKey, lastResultStreak, result.command, result.args, runErr, result.output)
				if actionSteps >= workerAssistNoNewEvidenceResultRepeat && lastResultStreak >= workerAssistNoNewEvidenceResultRepeat && isNoNewEvidenceCandidate(result.command, result.args) {
					if completionErr := emitNoNewEvidenceCompletion(turn, result.command, result.args, lastResultStreak, "identical runtime result repeated"); completionErr != nil {
						return completionErr
					}
					return nil
				}
				if toolCalls >= workerAssistNoNewEvidenceToolCallCap && recoveryTransitions > 0 && isNoNewEvidenceCandidate(result.command, result.args) {
					if completionErr := emitNoNewEvidenceCompletion(turn, result.command, result.args, lastResultStreak, "recover tool-call cap reached without new evidence"); completionErr != nil {
						return completionErr
					}
					return nil
				}
			}
			if runErr != nil {
				if isAssistInvalidToolSpecError(runErr) {
					mode = "recover"
					recoverHint = summarizeCommandResult(result.command, result.args, runErr, result.output)
					observations = appendObservation(observations, "recovery: invalid tool specification; request command/tool with concrete files+run")
					if recErr := markRecoverTransition(turn, "invalid_tool_spec", map[string]any{
						"command": result.command,
						"args":    result.args,
					}); recErr != nil {
						return recErr
					}
					continue
				}
				if strings.Contains(strings.ToLower(runErr.Error()), WorkerFailureScopeDenied) {
					_ = emitWorkerFailure(manager, cfg, task, runErr, WorkerFailureScopeDenied, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"command":    result.command,
						"args":       result.args,
						"log_path":   logPath,
					})
					return runErr
				}
				if mode == "recover" {
					reason := WorkerFailureCommandFailed
					if errorsIsTimeout(runErr) {
						reason = WorkerFailureCommandTimeout
					}
					_ = emitWorkerFailure(manager, cfg, task, runErr, reason, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"command":    result.command,
						"args":       result.args,
						"log_path":   logPath,
					})
					return runErr
				}
				mode = "recover"
				recoverHint = summarizeCommandResult(result.command, result.args, runErr, result.output)
				if recErr := markRecoverTransition(turn, "tool_execution_failed", map[string]any{
					"command": result.command,
					"args":    result.args,
				}); recErr != nil {
					return recErr
				}
				continue
			}
			if wasRecover {
				mode = "recover"
				recoverHint = "recovery mode active: use direct command or complete if evidence is sufficient"
			} else {
				mode = "execute-step"
				recoverHint = ""
				recoveryTransitions = 0
			}
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
			command, args = normalizeWorkerAssistCommand(command, args)
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
			args, injected, injectedTarget := applyCommandTargetFallback(scopePolicy, task, command, args)
			if injected {
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    fmt.Sprintf("auto-injected target %s for command %s", injectedTarget, command),
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
				})
			}
			var adapted bool
			var adaptNote string
			command, args, adaptNote, adapted = adaptCommandForRuntime(scopePolicy, command, args)
			if adapted && adaptNote != "" {
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    adaptNote,
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
					"command":    command,
					"args":       args,
				})
			}
			args, reinjected, reinjectedTarget := applyCommandTargetFallback(scopePolicy, task, command, args)
			if reinjected {
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    fmt.Sprintf("auto-injected target %s for command %s after runtime adaptation", reinjectedTarget, command),
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
					"command":    command,
					"args":       args,
				})
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
			key := buildAssistActionKey(command, args)
			lastActionKey, lastActionStreak = trackAssistActionStreak(lastActionKey, lastActionStreak, key)
			if lastActionStreak > workerAssistLoopMaxRepeat {
				wasRecover := mode == "recover"
				loopBlocks++
				mode = "recover"
				recoverHint = "same command repeated too many times; choose a different command path"
				observations = appendObservation(observations, fmt.Sprintf("recovery: repeated command blocked for %s", strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))))
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
			observations = appendObservation(observations, summarizeCommandResult(result.command, result.args, result.runErr, result.output))
			if result.runErr != nil {
				lastResultKey = ""
				lastResultStreak = 0
			} else {
				lastResultKey, lastResultStreak = trackAssistResultStreak(lastResultKey, lastResultStreak, result.command, result.args, result.runErr, result.output)
				if actionSteps >= workerAssistNoNewEvidenceResultRepeat && lastResultStreak >= workerAssistNoNewEvidenceResultRepeat && isNoNewEvidenceCandidate(result.command, result.args) {
					if completionErr := emitNoNewEvidenceCompletion(turn, result.command, result.args, lastResultStreak, "identical runtime result repeated"); completionErr != nil {
						return completionErr
					}
					return nil
				}
				if toolCalls >= workerAssistNoNewEvidenceToolCallCap && recoveryTransitions > 0 && isNoNewEvidenceCandidate(result.command, result.args) {
					if completionErr := emitNoNewEvidenceCompletion(turn, result.command, result.args, lastResultStreak, "recover tool-call cap reached without new evidence"); completionErr != nil {
						return completionErr
					}
					return nil
				}
			}
			if result.runErr != nil {
				if mode == "recover" {
					reason := WorkerFailureCommandFailed
					if errorsIsTimeout(result.runErr) {
						reason = WorkerFailureCommandTimeout
					}
					_ = emitWorkerFailure(manager, cfg, task, result.runErr, reason, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"command":    result.command,
						"args":       result.args,
						"log_path":   logPath,
					})
					return result.runErr
				}
				mode = "recover"
				recoverHint = summarizeCommandResult(result.command, result.args, result.runErr, result.output)
				if recErr := markRecoverTransition(turn, "command_execution_failed", map[string]any{
					"command": result.command,
					"args":    result.args,
				}); recErr != nil {
					return recErr
				}
				continue
			}
			if wasRecover {
				mode = "recover"
				recoverHint = "recovery mode active: use direct command or complete if evidence is sufficient"
			} else {
				mode = "execute-step"
				recoverHint = ""
				recoveryTransitions = 0
			}
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

func trackAssistActionStreak(lastKey string, lastStreak int, currentKey string) (string, int) {
	currentKey = strings.TrimSpace(currentKey)
	if currentKey == "" {
		return "", 0
	}
	if currentKey == lastKey {
		return currentKey, lastStreak + 1
	}
	return currentKey, 1
}

func trackAssistResultStreak(lastKey string, lastStreak int, command string, args []string, runErr error, output []byte) (string, int) {
	currentKey := buildAssistResultKey(command, args, runErr, output)
	if currentKey == "" {
		return "", 0
	}
	if currentKey == lastKey {
		return currentKey, lastStreak + 1
	}
	return currentKey, 1
}

func buildAssistResultKey(command string, args []string, runErr error, output []byte) string {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return ""
	}
	normalizedArgs := normalizeArgs(args)
	status := "ok"
	if runErr != nil {
		status = "err:" + strings.TrimSpace(runErr.Error())
	}
	fragment := strings.Join(strings.Fields(string(capBytes(output, workerAssistResultFingerprintBytes))), " ")
	return strings.Join([]string{command, strings.Join(normalizedArgs, " "), status, fragment}, "|")
}

func isAssistSummaryTask(task TaskSpec) bool {
	strategy := strings.ToLower(strings.TrimSpace(task.Strategy))
	switch strategy {
	case "summarize_and_replan", "summary", "report_summary":
		return true
	}
	if strings.EqualFold(strings.TrimSpace(task.TaskID), "task-plan-summary") {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(task.Title))
	return strings.Contains(title, "plan synthesis summary")
}

func isAssistAdaptiveReplanTask(task TaskSpec) bool {
	strategy := strings.ToLower(strings.TrimSpace(task.Strategy))
	return strings.HasPrefix(strategy, "adaptive_replan_")
}

func isNoNewEvidenceCandidate(command string, args []string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	if isNetworkSensitiveCommand(command) {
		return true
	}
	base := strings.ToLower(filepath.Base(command))
	switch base {
	case "list_dir", "ls", "dir", "read_file", "read":
		return true
	}
	if base != "bash" && base != "sh" && base != "zsh" {
		return false
	}
	if len(args) == 0 {
		return false
	}
	script := strings.TrimSpace(args[0])
	if script == "" || strings.HasPrefix(script, "-") {
		return false
	}
	if strings.Contains(filepath.ToSlash(script), "tools/") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(script))
	switch ext {
	case ".sh", ".bash", ".zsh", ".py", ".rb", ".pl":
		return true
	}
	return false
}

func buildAssistPromptScope(runScope Scope, taskTargets []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(runScope.Networks)+len(runScope.Targets)+len(runScope.DenyTargets))
	add := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	for _, network := range runScope.Networks {
		add(network)
	}
	for _, target := range runScope.Targets {
		add(target)
	}
	for _, deny := range runScope.DenyTargets {
		trimmed := strings.TrimSpace(deny)
		if trimmed == "" {
			continue
		}
		add("deny:" + trimmed)
	}
	if len(out) > 0 {
		return out
	}
	for _, target := range taskTargets {
		add(target)
	}
	return out
}

func buildAutonomousQuestionAnswer(task TaskSpec, question string, observations []string) string {
	parts := make([]string, 0, 4)
	if trimmed := strings.TrimSpace(question); trimmed != "" {
		parts = append(parts, "question="+trimmed)
	}
	if len(task.Targets) > 0 {
		parts = append(parts, "targets="+strings.Join(task.Targets, ", "))
	}
	if len(task.DoneWhen) > 0 {
		parts = append(parts, "done_when="+strings.Join(task.DoneWhen, "; "))
	}
	if len(observations) > 0 {
		latest := strings.TrimSpace(observations[len(observations)-1])
		if latest != "" {
			if len(latest) > 240 {
				latest = latest[:240] + "..."
			}
			parts = append(parts, "latest_observation="+latest)
		}
	}
	return strings.Join(parts, " | ")
}

func resolveShellToolScriptPath(workDir, command string, args []string) ([]string, string, bool) {
	if len(args) == 0 {
		return args, "", false
	}
	shell := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	switch shell {
	case "bash", "sh", "zsh":
	default:
		return args, "", false
	}
	script := strings.TrimSpace(args[0])
	if script == "" || strings.HasPrefix(script, "-") {
		return args, "", false
	}
	if strings.Contains(script, "/") || strings.Contains(script, string(os.PathSeparator)) {
		return args, "", false
	}
	if !strings.HasSuffix(strings.ToLower(script), ".sh") {
		return args, "", false
	}
	if _, err := os.Stat(filepath.Join(workDir, script)); err == nil {
		return args, "", false
	}
	candidate := filepath.Join(workDir, "tools", script)
	if _, err := os.Stat(candidate); err != nil {
		return args, "", false
	}
	updated := append([]string{}, args...)
	updated[0] = filepath.ToSlash(filepath.Join("tools", script))
	note := fmt.Sprintf("resolved shell script %s to %s from worker tools directory", script, updated[0])
	return updated, note, true
}
