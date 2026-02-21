package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

const (
	workerLLMBaseURLEnv       = "BIRDHACKBOT_LLM_BASE_URL"
	workerLLMModelEnv         = "BIRDHACKBOT_LLM_MODEL"
	workerLLMAPIKeyEnv        = "BIRDHACKBOT_LLM_API_KEY"
	workerLLMTimeoutSeconds   = "BIRDHACKBOT_LLM_TIMEOUT_SECONDS"
	workerAssistModeEnv       = "BIRDHACKBOT_WORKER_ASSIST_MODE"
	workerConfigPathEnv       = "BIRDHACKBOT_CONFIG_PATH"
	workerAssistObsLimit      = 12
	workerAssistOutputLimit   = 24_000
	workerAssistReadMaxBytes  = 64_000
	workerAssistWriteMaxBytes = 512_000
	workerAssistBrowseMaxBody = 120_000
	workerAssistLoopMaxRepeat = 3
	workerAssistLLMCallMax    = 90 * time.Second
	workerAssistLLMCallMin    = 15 * time.Second
	workerAssistBudgetReserve = 10 * time.Second
	workerAssistMinTurns      = 16
	workerAssistTurnFactor    = 4
	workerAssistMaxLoopBlocks = 6
	workerAssistMaxRecoveries = 4
)

var htmlTitlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

type workerToolResult struct {
	command string
	args    []string
	output  []byte
	runErr  error
}

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
	mode := "execute-step"
	recoverHint := ""
	toolCalls := 0
	actionSteps := 0
	loopBlocks := 0
	questionLoops := 0
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

	for turn := 1; turn <= maxTurns; turn++ {
		if actionSteps >= maxActionSteps {
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
				if recErr := markRecoverTransition(turn, "invalid_suggestion_schema", map[string]any{
					"schema_error": schemaErr.Error(),
				}); recErr != nil {
					return recErr
				}
				if loopBlocks >= workerAssistMaxLoopBlocks {
					_ = emitWorkerFailure(manager, cfg, task, schemaErr, WorkerFailureAssistNoAction, map[string]any{
						"step":         actionSteps,
						"turn":         turn,
						"tool_calls":   toolCalls,
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
			final := strings.TrimSpace(suggestion.Final)
			if final == "" {
				final = strings.TrimSpace(suggestion.Summary)
			}
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
				"step":      actionSteps,
				"tool_call": toolCalls,
			})
			_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskFinding, map[string]any{
				"target":       primaryTaskTarget(task),
				"finding_type": "task_execution_result",
				"title":        "task action completed",
				"severity":     "info",
				"confidence":   "medium",
				"source":       "worker_runtime_assist",
				"evidence":     []string{logPath},
				"metadata": map[string]any{
					"attempt":       cfg.Attempt,
					"reason":        "assist_complete",
					"assistant":     assistantModel,
					"steps":         actionSteps,
					"turns":         turn,
					"tool_calls":    toolCalls,
					"final_summary": final,
				},
			})
			_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskCompleted, map[string]any{
				"attempt":   cfg.Attempt,
				"worker_id": cfg.WorkerID,
				"reason":    "assist_complete",
				"log_path":  logPath,
				"completion_contract": map[string]any{
					"status_reason":       "assist_complete",
					"required_artifacts":  task.ExpectedArtifacts,
					"produced_artifacts":  []string{logPath},
					"required_findings":   []string{"task_execution_result"},
					"produced_findings":   []string{"task_execution_result"},
					"verification_status": "reported_by_worker",
				},
			})
			return nil
		case "question":
			questionLoops++
			question := strings.TrimSpace(suggestion.Question)
			observations = appendObservation(observations, "assistant asked for input in non-interactive worker mode: "+question)
			_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
				"message":    "assistant asked a question; worker is non-interactive, forcing autonomous recovery",
				"step":       actionSteps,
				"turn":       turn,
				"tool_calls": toolCalls,
				"mode":       mode,
				"question":   question,
			})
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
			recoverHint = "No user interaction is available in worker mode. Infer missing values from task target, prior logs, and artifacts, then proceed with a concrete command."
			if recErr := markRecoverTransition(turn, "question_without_interaction", map[string]any{
				"question": question,
			}); recErr != nil {
				return recErr
			}
			continue
		case "plan", "noop":
			questionLoops = 0
			if mode == "recover" {
				err := fmt.Errorf("assistant did not provide actionable recovery step")
				_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistNoAction, map[string]any{
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"type":       suggestion.Type,
				})
				return err
			}
			mode = "execute-step"
			observations = appendObservation(observations, summarizeSuggestion(suggestion))
			continue
		case "tool":
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
			actionKey := buildAssistActionKey(runCommand, runArgs)
			lastActionKey, lastActionStreak = trackAssistActionStreak(lastActionKey, lastActionStreak, actionKey)
			if lastActionStreak > workerAssistLoopMaxRepeat {
				loopBlocks++
				mode = "recover"
				recoverHint = "same action repeated too many times; choose a different command path"
				observations = appendObservation(observations, fmt.Sprintf("recovery: repeated action blocked for %s", strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))))
				if recErr := markRecoverTransition(turn, "repeated_action", map[string]any{
					"command": runCommand,
					"args":    runArgs,
					"streak":  lastActionStreak,
				}); recErr != nil {
					return recErr
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
			mode = "execute-step"
			recoverHint = ""
			recoveryTransitions = 0
		case "command":
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
			if !isBuiltinListDir(command) && !isBuiltinReadFile(command) && !isBuiltinWriteFile(command) {
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
				loopBlocks++
				mode = "recover"
				recoverHint = "same command repeated too many times; choose a different command path"
				observations = appendObservation(observations, fmt.Sprintf("recovery: repeated command blocked for %s", strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))))
				if recErr := markRecoverTransition(turn, "repeated_command", map[string]any{
					"command": command,
					"args":    args,
					"streak":  lastActionStreak,
				}); recErr != nil {
					return recErr
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
			actionSteps++
			toolCalls++
			loopBlocks = 0
			questionLoops = 0
			result := executeWorkerAssistCommand(ctx, command, args, workDir)
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
			mode = "execute-step"
			recoverHint = ""
			recoveryTransitions = 0
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
