package orchestrator

import "strings"

type runtimeMutationNote struct {
	Stage   string
	Message string
}

func emitRuntimePreparationProgress(
	manager *Manager,
	runID string,
	workerSignalID string,
	taskID string,
	step int,
	turn int,
	toolCount int,
	toolCountField string,
	mode string,
	command string,
	args []string,
	messages []string,
) {
	if toolCountField == "" {
		toolCountField = "tool_call"
	}
	for _, message := range messages {
		emitRuntimeMutationProgress(
			manager,
			runID,
			workerSignalID,
			taskID,
			step,
			turn,
			toolCount,
			toolCountField,
			mode,
			command,
			args,
			"prepare_runtime_command",
			message,
		)
	}
}

func emitRuntimeMutationProgress(
	manager *Manager,
	runID string,
	workerSignalID string,
	taskID string,
	step int,
	turn int,
	toolCount int,
	toolCountField string,
	mode string,
	command string,
	args []string,
	stage string,
	message string,
) {
	if manager == nil || strings.TrimSpace(message) == "" {
		return
	}
	if strings.TrimSpace(toolCountField) == "" {
		toolCountField = "tool_call"
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = "runtime_mutation"
	}
	payload := map[string]any{
		"message":         message,
		"step":            step,
		"command":         command,
		"args":            args,
		"decision_source": decisionSourceRuntimeAdapt,
		"runtime_stage":   stage,
	}
	payload[toolCountField] = toolCount
	if turn > 0 {
		payload["turn"] = turn
	}
	if mode != "" {
		payload["mode"] = mode
	}
	_ = manager.EmitEvent(runID, workerSignalID, taskID, EventTypeTaskProgress, payload)
}

func emitRuntimeMutationNotes(
	manager *Manager,
	runID string,
	workerSignalID string,
	taskID string,
	step int,
	turn int,
	toolCount int,
	toolCountField string,
	mode string,
	command string,
	args []string,
	notes []runtimeMutationNote,
) {
	for _, note := range notes {
		payload := map[string]any{
			"stage":   strings.TrimSpace(note.Stage),
			"message": strings.TrimSpace(note.Message),
		}
		if payload["stage"] == "" || payload["message"] == "" {
			continue
		}
		emitRuntimeMutationProgress(
			manager,
			runID,
			workerSignalID,
			taskID,
			step,
			turn,
			toolCount,
			toolCountField,
			mode,
			command,
			args,
			payload["stage"].(string),
			payload["message"].(string),
		)
	}
}
