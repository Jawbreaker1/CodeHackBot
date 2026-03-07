package orchestrator

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/msf"
)

type runtimePipelineResult struct {
	Command string
	Args    []string
	Notes   []runtimeMutationNote
}

func applyRuntimeCommandPipeline(
	cfg WorkerRunConfig,
	task TaskSpec,
	scopePolicy *ScopePolicy,
	command string,
	args []string,
	requiredAttribution targetAttribution,
	goal string,
	workDir string,
) (runtimePipelineResult, error) {
	result := runtimePipelineResult{
		Command: command,
		Args:    append([]string{}, args...),
		Notes:   []runtimeMutationNote{},
	}
	assistMode := strings.EqualFold(strings.TrimSpace(task.Action.Type), "assist")
	if cfg.Diagnostic {
		return result, nil
	}
	if nextArgs, note, rewritten := enforceAttributedCommandTarget(result.Command, result.Args, requiredAttribution.Target); rewritten {
		result.Args = nextArgs
		result.Notes = append(result.Notes, runtimeMutationNote{
			Stage:   "target_attribution",
			Message: note,
		})
	}

	if !assistMode {
		prepared := prepareRuntimeCommand(scopePolicy, task, result.Command, result.Args)
		result.Command = prepared.Command
		result.Args = prepared.Args
		for _, msg := range runtimePreparationMessages(prepared) {
			result.Notes = append(result.Notes, runtimeMutationNote{
				Stage:   "prepare_runtime_command",
				Message: msg,
			})
		}

		if nextCommand, nextArgs, note, rewritten := adaptWeakReportAction(cfg, task, scopePolicy, result.Command, result.Args, requiredAttribution); rewritten {
			result.Command = nextCommand
			result.Args = nextArgs
			result.Notes = append(result.Notes, runtimeMutationNote{
				Stage:   "adapt_weak_report_action",
				Message: note,
			})
		}
		if nextArgs, note, rewritten := ensureVulnerabilityEvidenceActionWithGoal(task, goal, result.Command, result.Args); rewritten {
			result.Args = nextArgs
			result.Notes = append(result.Notes, runtimeMutationNote{
				Stage:   "enforce_vulnerability_evidence",
				Message: note,
			})
		}
		if nextCommand, nextArgs, note, rewritten := adaptWeakVulnerabilityAction(task, scopePolicy, result.Command, result.Args); rewritten {
			result.Command = nextCommand
			result.Args = nextArgs
			result.Notes = append(result.Notes, runtimeMutationNote{
				Stage:   "adapt_weak_vulnerability_action",
				Message: note,
			})
		}
	}
	if strings.TrimSpace(workDir) == "" {
		return result, nil
	}

	adaptedCmd, adaptedArgs, adaptNotes, adaptErr := msf.AdaptRuntimeCommand(result.Command, result.Args, workDir)
	if adaptErr != nil {
		return result, adaptErr
	}
	result.Command = adaptedCmd
	result.Args = adaptedArgs
	appendRuntimeMutationMessages(&result, "adapt_runtime_command", adaptNotes)

	if repairedArgs, repairNotes, repaired, repairErr := repairMissingCommandInputPaths(cfg, task, result.Command, result.Args); repairErr != nil {
		result.Notes = append(result.Notes, runtimeMutationNote{
			Stage:   "repair_missing_inputs",
			Message: fmt.Sprintf("runtime input repair skipped: %v", repairErr),
		})
	} else if repaired {
		result.Args = repairedArgs
		appendRuntimeMutationMessages(&result, "repair_missing_inputs", repairNotes)
	}

	if !assistMode {
		if adaptedCommand, adaptedArgs, archiveNotes, adapted, archiveErr := adaptArchiveWorkflowCommand(cfg, task, result.Command, result.Args, workDir); archiveErr != nil {
			result.Notes = append(result.Notes, runtimeMutationNote{
				Stage:   "adapt_archive_workflow",
				Message: fmt.Sprintf("archive workflow adaptation skipped: %v", archiveErr),
			})
		} else if adapted {
			result.Command = adaptedCommand
			result.Args = adaptedArgs
			appendRuntimeMutationMessages(&result, "adapt_archive_workflow", archiveNotes)
		}
	}

	return result, nil
}

func appendRuntimeMutationMessages(result *runtimePipelineResult, stage string, messages []string) {
	if result == nil {
		return
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = "runtime_mutation"
	}
	for _, message := range messages {
		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}
		result.Notes = append(result.Notes, runtimeMutationNote{
			Stage:   stage,
			Message: message,
		})
	}
}
