package orchestrator

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
		payload := map[string]any{
			"message": message,
			"step":    step,
			"command": command,
			"args":    args,
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
}
