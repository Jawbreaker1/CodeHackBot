package orchestrator

func updateAssistResultStreakAndCheckNoEvidence(
	manager *Manager,
	cfg WorkerRunConfig,
	task TaskSpec,
	contextEnvelope *workerAssistContextEnvelope,
	assistantModel string,
	mode string,
	actionSteps int,
	turn int,
	toolCalls int,
	recoveryTransitions int,
	command string,
	args []string,
	runErr error,
	output []byte,
	lastResultKey string,
	lastResultStreak int,
) (string, int, bool, error) {
	if runErr != nil {
		return "", 0, false, nil
	}
	lastResultKey, lastResultStreak = trackAssistResultStreak(lastResultKey, lastResultStreak, command, args, runErr, output)
	contextEnvelope.recordResultFingerprint(lastResultKey, lastResultStreak)
	if actionSteps >= workerAssistNoNewEvidenceResultRepeat &&
		lastResultStreak >= workerAssistNoNewEvidenceResultRepeat &&
		isNoNewEvidenceCandidate(command, args) {
		if completionErr := emitAssistNoNewEvidenceCompletion(manager, cfg, task, assistantModel, mode, actionSteps, turn, toolCalls, command, args, lastResultStreak, "identical runtime result repeated"); completionErr != nil {
			return lastResultKey, lastResultStreak, false, completionErr
		}
		return lastResultKey, lastResultStreak, true, nil
	}
	if toolCalls >= workerAssistNoNewEvidenceToolCallCap &&
		recoveryTransitions > 0 &&
		isNoNewEvidenceCandidate(command, args) {
		if completionErr := emitAssistNoNewEvidenceCompletion(manager, cfg, task, assistantModel, mode, actionSteps, turn, toolCalls, command, args, lastResultStreak, "recover tool-call cap reached without new evidence"); completionErr != nil {
			return lastResultKey, lastResultStreak, false, completionErr
		}
		return lastResultKey, lastResultStreak, true, nil
	}
	return lastResultKey, lastResultStreak, false, nil
}
