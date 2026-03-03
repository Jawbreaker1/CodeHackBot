package orchestrator

func settleAssistModeAfterSuccessfulExecution(mode *string, recoverHint *string, recoveryTransitions *int, wasRecover bool) {
	if wasRecover {
		*mode = "recover"
		*recoverHint = "recovery mode active: use direct command or complete if evidence is sufficient"
		return
	}
	*mode = "execute-step"
	*recoverHint = ""
	*recoveryTransitions = 0
}
