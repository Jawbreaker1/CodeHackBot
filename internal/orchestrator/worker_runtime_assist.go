package orchestrator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

const (
	workerLLMBaseURLEnv       = "BIRDHACKBOT_LLM_BASE_URL"
	workerLLMModelEnv         = "BIRDHACKBOT_LLM_MODEL"
	workerLLMAPIKeyEnv        = "BIRDHACKBOT_LLM_API_KEY"
	workerLLMTimeoutSeconds   = "BIRDHACKBOT_LLM_TIMEOUT_SECONDS"
	workerConfigPathEnv       = "BIRDHACKBOT_CONFIG_PATH"
	workerAssistObsLimit      = 12
	workerAssistOutputLimit   = 24_000
	workerAssistReadMaxBytes  = 64_000
	workerAssistWriteMaxBytes = 512_000
	workerAssistBrowseMaxBody = 120_000
	workerAssistLoopMaxRepeat = 2
	workerAssistLLMCallMax    = 45 * time.Second
	workerAssistLLMCallMin    = 8 * time.Second
	workerAssistBudgetReserve = 5 * time.Second
	workerAssistMinTurns      = 16
	workerAssistTurnFactor    = 4
	workerAssistMaxLoopBlocks = 3
)

var htmlTitlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

type workerToolResult struct {
	command string
	args    []string
	output  []byte
	runErr  error
}

func runWorkerAssistTask(ctx context.Context, manager *Manager, cfg WorkerRunConfig, task TaskSpec, action TaskAction, scopePolicy *ScopePolicy, workDir string) error {
	assistantModel, assistant, err := buildWorkerAssistant()
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
	executed := map[string]int{}
	blockedActions := map[string]struct{}{}
	mode := "execute-step"
	recoverHint := ""
	toolCalls := 0
	actionSteps := 0
	loopBlocks := 0

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
		suggestion, suggestErr := assistant.Suggest(suggestCtx, assist.Input{
			SessionID:   cfg.RunID,
			Scope:       task.Targets,
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
			}
			_ = emitWorkerFailure(manager, cfg, task, suggestErr, failureReason, map[string]any{
				"step":                     actionSteps,
				"turn":                     turn,
				"tool_calls":               toolCalls,
				"mode":                     mode,
				"llm_timeout_seconds":      int(callTimeout.Seconds()),
				"remaining_budget_seconds": int(remainingBudget.Seconds()),
			})
			return suggestErr
		}

		progressMsg := strings.TrimSpace(suggestion.Summary)
		if progressMsg == "" {
			progressMsg = fmt.Sprintf("assistant returned %s", suggestion.Type)
		}
		_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
			"message":    progressMsg,
			"step":       actionSteps,
			"turn":       turn,
			"tool_calls": toolCalls,
			"mode":       mode,
			"type":       suggestion.Type,
		})

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
					"verification_status": "satisfied",
				},
			})
			return nil
		case "question":
			err := fmt.Errorf("assistant requested user input: %s", strings.TrimSpace(suggestion.Question))
			_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistNeedsInput, map[string]any{
				"step":       actionSteps,
				"turn":       turn,
				"tool_calls": toolCalls,
				"question":   strings.TrimSpace(suggestion.Question),
			})
			return err
		case "plan", "noop":
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
				err := fmt.Errorf("assistant tool suggestion missing spec")
				_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistNoAction, map[string]any{
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
				})
				return err
			}
			runCommand := strings.TrimSpace(suggestion.Tool.Run.Command)
			runArgs := append([]string{}, suggestion.Tool.Run.Args...)
			actionKey := buildAssistActionKey(runCommand, runArgs)
			if _, blocked := blockedActions[actionKey]; blocked {
				loopBlocks++
				mode = "recover"
				recoverHint = "action blocked as repeated; choose a different command path"
				observations = appendObservation(observations, fmt.Sprintf("recovery: repeated action blocked for %s", strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))))
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    "repeated action blocked; waiting for alternative command",
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
				})
				if loopBlocks >= workerAssistMaxLoopBlocks {
					err := fmt.Errorf("repeated command blocked too many times")
					_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistLoopDetected, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"command":    runCommand,
						"args":       runArgs,
					})
					return err
				}
				continue
			}
			executed[actionKey]++
			if executed[actionKey] > workerAssistLoopMaxRepeat {
				blockedActions[actionKey] = struct{}{}
				loopBlocks++
				mode = "recover"
				recoverHint = "action blocked as repeated; choose a different command path"
				observations = appendObservation(observations, fmt.Sprintf("recovery: repeated action blocked for %s", strings.TrimSpace(strings.Join(append([]string{runCommand}, runArgs...), " "))))
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    "repeated action blocked; waiting for alternative command",
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
				})
				continue
			}
			actionSteps++
			toolCalls++
			loopBlocks = 0
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
				continue
			}
			mode = "execute-step"
			recoverHint = ""
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
			if _, blocked := blockedActions[key]; blocked {
				loopBlocks++
				mode = "recover"
				recoverHint = "action blocked as repeated; choose a different command path"
				observations = appendObservation(observations, fmt.Sprintf("recovery: repeated command blocked for %s", strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))))
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    "repeated command blocked; waiting for alternative",
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
				})
				if loopBlocks >= workerAssistMaxLoopBlocks {
					err := fmt.Errorf("repeated command blocked too many times")
					_ = emitWorkerFailure(manager, cfg, task, err, WorkerFailureAssistLoopDetected, map[string]any{
						"step":       actionSteps,
						"turn":       turn,
						"tool_calls": toolCalls,
						"command":    command,
						"args":       args,
					})
					return err
				}
				continue
			}
			executed[key]++
			if executed[key] > workerAssistLoopMaxRepeat {
				blockedActions[key] = struct{}{}
				loopBlocks++
				mode = "recover"
				recoverHint = "action blocked as repeated; choose a different command path"
				observations = appendObservation(observations, fmt.Sprintf("recovery: repeated command blocked for %s", strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))))
				_ = manager.EmitEvent(cfg.RunID, WorkerSignalID(cfg.WorkerID), cfg.TaskID, EventTypeTaskProgress, map[string]any{
					"message":    "repeated command blocked; waiting for alternative",
					"step":       actionSteps,
					"turn":       turn,
					"tool_calls": toolCalls,
					"mode":       mode,
				})
				continue
			}
			actionSteps++
			toolCalls++
			loopBlocks = 0
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
				continue
			}
			mode = "execute-step"
			recoverHint = ""
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

func buildWorkerAssistant() (string, assist.Assistant, error) {
	cfg, err := loadWorkerLLMConfig()
	if err != nil {
		return "", nil, err
	}
	model := strings.TrimSpace(cfg.LLM.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.Agent.Model)
	}
	if model == "" {
		return "", nil, fmt.Errorf("llm model is required (set %s or config llm.model)", workerLLMModelEnv)
	}
	client := llm.NewLMStudioClient(cfg)
	return model, assist.LLMAssistant{Client: client, Model: model}, nil
}

func loadWorkerLLMConfig() (config.Config, error) {
	loaded := config.Config{}
	loaded.LLM.TimeoutSeconds = 120

	loadPath := strings.TrimSpace(os.Getenv(workerConfigPathEnv))
	if loadPath == "" {
		pwd := strings.TrimSpace(os.Getenv("PWD"))
		if pwd != "" {
			candidate := filepath.Join(pwd, "config", "default.json")
			if _, err := os.Stat(candidate); err == nil {
				loadPath = candidate
			}
		}
	}
	if loadPath != "" {
		cfg, _, err := config.Load(loadPath, "", "")
		if err != nil {
			return config.Config{}, fmt.Errorf("load config from %s: %w", loadPath, err)
		}
		loaded = cfg
		if loaded.LLM.TimeoutSeconds <= 0 {
			loaded.LLM.TimeoutSeconds = 120
		}
	}

	if v := strings.TrimSpace(os.Getenv(workerLLMBaseURLEnv)); v != "" {
		loaded.LLM.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv(workerLLMModelEnv)); v != "" {
		loaded.LLM.Model = v
	}
	if v := strings.TrimSpace(os.Getenv(workerLLMAPIKeyEnv)); v != "" {
		loaded.LLM.APIKey = v
	}
	if v := strings.TrimSpace(os.Getenv(workerLLMTimeoutSeconds)); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			loaded.LLM.TimeoutSeconds = parsed
		}
	}
	return loaded, nil
}

func executeWorkerAssistCommand(ctx context.Context, command string, args []string, workDir string) workerToolResult {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)

	if isBuiltinListDir(command) {
		output, err := builtinListDir(args, workDir)
		return workerToolResult{command: "list_dir", args: args, output: output, runErr: err}
	}
	if isBuiltinReadFile(command) {
		output, err := builtinReadFile(args, workDir)
		return workerToolResult{command: "read_file", args: args, output: output, runErr: err}
	}
	if isBuiltinWriteFile(command) {
		output, err := builtinWriteFile(args, workDir)
		return workerToolResult{command: "write_file", args: args, output: output, runErr: err}
	}
	if isBuiltinBrowse(command) {
		output, err := builtinBrowse(ctx, args)
		return workerToolResult{command: "browse", args: args, output: output, runErr: err}
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	output, err := runWorkerCommand(ctx, cmd, workerCommandStopGrace)
	return workerToolResult{command: command, args: args, output: output, runErr: err}
}

func executeWorkerAssistTool(ctx context.Context, cfg WorkerRunConfig, task TaskSpec, scopePolicy *ScopePolicy, workDir string, spec *assist.ToolSpec) (workerToolResult, error) {
	if spec == nil {
		return workerToolResult{}, fmt.Errorf("tool suggestion missing spec")
	}
	runCommand := strings.TrimSpace(spec.Run.Command)
	if runCommand == "" {
		return workerToolResult{}, fmt.Errorf("tool suggestion missing run.command")
	}
	files := spec.Files
	if len(files) == 0 {
		return workerToolResult{}, fmt.Errorf("tool suggestion has no files")
	}
	if len(files) > 20 {
		return workerToolResult{}, fmt.Errorf("tool suggestion too many files (%d > 20)", len(files))
	}

	toolRoot := filepath.Join(workDir, "tools")
	if err := os.MkdirAll(toolRoot, 0o755); err != nil {
		return workerToolResult{}, fmt.Errorf("create tool root: %w", err)
	}

	var builder strings.Builder
	for _, file := range files {
		relPath := strings.TrimSpace(file.Path)
		if relPath == "" {
			continue
		}
		if filepath.IsAbs(relPath) {
			return workerToolResult{}, fmt.Errorf("tool file must be relative: %s", relPath)
		}
		dst := filepath.Clean(filepath.Join(toolRoot, relPath))
		if !pathWithinRoot(dst, toolRoot) {
			return workerToolResult{}, fmt.Errorf("tool file escapes tool root: %s", relPath)
		}
		if len(file.Content) > workerAssistWriteMaxBytes {
			return workerToolResult{}, fmt.Errorf("tool file too large: %s", relPath)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return workerToolResult{}, fmt.Errorf("create tool parent dir: %w", err)
		}
		if err := os.WriteFile(dst, []byte(file.Content), 0o644); err != nil {
			return workerToolResult{}, fmt.Errorf("write tool file: %w", err)
		}
		_, _ = fmt.Fprintf(&builder, "Wrote tool file: %s\n", dst)
	}

	args := normalizeArgs(spec.Run.Args)
	runCommand, args = normalizeWorkerAssistCommand(runCommand, args)
	args, _, _ = applyCommandTargetFallback(scopePolicy, task, runCommand, args)
	if err := scopePolicy.ValidateCommandTargets(runCommand, args); err != nil {
		return workerToolResult{}, fmt.Errorf("%s: %w", WorkerFailureScopeDenied, err)
	}
	cmd := exec.Command(runCommand, args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	output, runErr := runWorkerCommand(ctx, cmd, workerCommandStopGrace)
	builder.Write(output)
	return workerToolResult{
		command: runCommand,
		args:    args,
		output:  capBytes([]byte(builder.String()), workerAssistOutputLimit),
		runErr:  runErr,
	}, nil
}

func normalizeWorkerAssistCommand(command string, args []string) (string, []string) {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	if command == "" {
		return "", args
	}
	parts := strings.Fields(command)
	if len(parts) > 1 && len(args) == 0 {
		return parts[0], parts[1:]
	}
	return command, args
}

func applyCommandTargetFallback(scopePolicy *ScopePolicy, task TaskSpec, command string, args []string) ([]string, bool, string) {
	if scopePolicy == nil {
		return args, false, ""
	}
	command = strings.TrimSpace(command)
	if command == "" || !isNetworkSensitiveCommand(command) {
		return args, false, ""
	}
	if len(scopePolicy.extractTargets(command, args)) > 0 {
		return args, false, ""
	}
	fallback := firstTaskTarget(task.Targets)
	if fallback == "" {
		return args, false, ""
	}
	updated := append(append([]string{}, args...), fallback)
	return updated, true, fallback
}

func firstTaskTarget(targets []string) string {
	for _, target := range targets {
		trimmed := strings.TrimSpace(target)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func writeWorkerActionLog(cfg WorkerRunConfig, filename string, output []byte) (string, error) {
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	if strings.TrimSpace(filename) == "" {
		filename = fmt.Sprintf("%s-a%d.log", sanitizePathComponent(cfg.WorkerID), cfg.Attempt)
	}
	path := filepath.Join(artifactDir, sanitizePathComponent(filename))
	if err := os.WriteFile(path, capBytes(output, workerAssistOutputLimit), 0o644); err != nil {
		return "", fmt.Errorf("write action log: %w", err)
	}
	return path, nil
}

func summarizeCommandResult(command string, args []string, runErr error, output []byte) string {
	joined := strings.TrimSpace(strings.Join(append([]string{strings.TrimSpace(command)}, args...), " "))
	if joined == "" {
		joined = "(command)"
	}
	if runErr != nil {
		return fmt.Sprintf("command failed: %s | %v | output: %s", joined, runErr, summarizeOutput(output))
	}
	return fmt.Sprintf("command ok: %s | output: %s", joined, summarizeOutput(output))
}

func summarizeSuggestion(suggestion assist.Suggestion) string {
	parts := []string{suggestion.Type}
	if s := strings.TrimSpace(suggestion.Summary); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(suggestion.Plan); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " | ")
}

func summarizeOutput(output []byte) string {
	text := strings.TrimSpace(string(capBytes(output, 800)))
	if text == "" {
		return "(no output)"
	}
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 300 {
		return text[:300] + "..."
	}
	return text
}

func appendObservation(observations []string, entry string) []string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return observations
	}
	observations = append(observations, entry)
	if len(observations) <= workerAssistObsLimit {
		return observations
	}
	return append([]string{}, observations[len(observations)-workerAssistObsLimit:]...)
}

func capBytes(data []byte, max int) []byte {
	if max <= 0 || len(data) <= max {
		return data
	}
	out := make([]byte, max)
	copy(out, data[:max])
	return out
}

func buildAssistActionKey(command string, args []string) string {
	parts := append([]string{strings.ToLower(strings.TrimSpace(command))}, args...)
	return strings.Join(parts, "\x1f")
}

func errorsIsTimeout(err error) bool {
	return err == errWorkerCommandTimeout || strings.Contains(strings.ToLower(err.Error()), "timeout")
}

func isAssistTimeoutError(suggestErr error, suggestCtxErr error, runCtxErr error) bool {
	if suggestErr == nil {
		return false
	}
	if suggestCtxErr == context.DeadlineExceeded || runCtxErr == context.DeadlineExceeded {
		return true
	}
	lower := strings.ToLower(suggestErr.Error())
	return strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "timeout")
}

func newAssistCallContext(runCtx context.Context) (context.Context, context.CancelFunc, time.Duration, time.Duration, error) {
	remaining := workerAssistLLMCallMax
	if deadline, ok := runCtx.Deadline(); ok {
		remaining = time.Until(deadline)
	}
	if remaining <= workerAssistBudgetReserve+workerAssistLLMCallMin {
		remainingSecs := int(remaining.Seconds())
		if remainingSecs < 0 {
			remainingSecs = 0
		}
		return runCtx, func() {}, 0, remaining, fmt.Errorf("assist call timeout: remaining budget too low (%ds)", remainingSecs)
	}
	callTimeout := workerAssistLLMCallMax
	available := remaining - workerAssistBudgetReserve
	if available < callTimeout {
		callTimeout = available
	}
	if callTimeout < workerAssistLLMCallMin {
		callTimeout = workerAssistLLMCallMin
	}
	ctx, cancel := context.WithTimeout(runCtx, callTimeout)
	return ctx, cancel, callTimeout, remaining, nil
}

func isBuiltinListDir(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "list_dir", "ls", "dir":
		return true
	default:
		return false
	}
}

func isBuiltinReadFile(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "read_file", "read":
		return true
	default:
		return false
	}
}

func isBuiltinWriteFile(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "write_file", "write":
		return true
	default:
		return false
	}
}

func isBuiltinBrowse(command string) bool {
	return strings.EqualFold(strings.TrimSpace(command), "browse")
}

func builtinListDir(args []string, workDir string) ([]byte, error) {
	target := "."
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		target = arg
		break
	}
	path := target
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []byte(fmt.Sprintf("Path: %s\nType: file\nName: %s\nBytes: %d\n", path, info.Name(), info.Size())), nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Path: %s\nEntries: %d\n", path, len(names))
	for _, name := range names {
		b.WriteString("- " + name + "\n")
	}
	return capBytes([]byte(b.String()), workerAssistOutputLimit), nil
}

func builtinReadFile(args []string, workDir string) ([]byte, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, fmt.Errorf("read_file requires a path argument")
	}
	path := strings.TrimSpace(args[0])
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	path = filepath.Clean(path)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, workerAssistReadMaxBytes))
	if err != nil {
		return nil, err
	}
	return capBytes(data, workerAssistOutputLimit), nil
}

func builtinWriteFile(args []string, workDir string) ([]byte, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("write_file requires <path> <content>")
	}
	target := strings.TrimSpace(args[0])
	if target == "" {
		return nil, fmt.Errorf("write_file target is empty")
	}
	if filepath.IsAbs(target) {
		return nil, fmt.Errorf("write_file requires relative path")
	}
	path := filepath.Clean(filepath.Join(workDir, target))
	if !pathWithinRoot(path, workDir) {
		return nil, fmt.Errorf("write_file path escapes workspace")
	}
	content := strings.Join(args[1:], " ")
	if len(content) > workerAssistWriteMaxBytes {
		return nil, fmt.Errorf("write_file content too large")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("Wrote %d bytes to %s\n", len(content), path)), nil
}

func pathWithinRoot(candidate, root string) bool {
	candidate = filepath.Clean(candidate)
	root = filepath.Clean(root)
	if candidate == root {
		return true
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func builtinBrowse(ctx context.Context, args []string) ([]byte, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, fmt.Errorf("browse requires a URL")
	}
	target := strings.TrimSpace(args[0])
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "BirdHackBot-Orchestrator/1.0")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, workerAssistBrowseMaxBody))
	if err != nil {
		return nil, err
	}
	title := ""
	if match := htmlTitlePattern.FindSubmatch(body); len(match) > 1 {
		title = strings.TrimSpace(strings.ReplaceAll(string(match[1]), "\n", " "))
	}
	snippet := strings.TrimSpace(string(body))
	snippet = strings.Join(strings.Fields(snippet), " ")
	if len(snippet) > 800 {
		snippet = snippet[:800] + "..."
	}
	var out strings.Builder
	_, _ = fmt.Fprintf(&out, "URL: %s\nStatus: %d\n", target, resp.StatusCode)
	if title != "" {
		_, _ = fmt.Fprintf(&out, "Title: %s\n", title)
	}
	if snippet != "" {
		_, _ = fmt.Fprintf(&out, "Snippet: %s\n", snippet)
	}
	return capBytes([]byte(out.String()), workerAssistOutputLimit), nil
}
