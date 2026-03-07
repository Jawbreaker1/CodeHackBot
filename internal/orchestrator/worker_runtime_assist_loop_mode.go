package orchestrator

import "strings"

func settleAssistModeAfterSuccessfulExecution(mode *string, recoverHint *string, recoveryTransitions *int, wasRecover bool, lastSummary string, lastExecFeedback *assistExecutionFeedback) {
	if wasRecover {
		*mode = "recover"
		*recoverHint = buildRecoverSuccessHint(lastSummary, lastExecFeedback)
		return
	}
	*mode = "execute-step"
	*recoverHint = ""
	*recoveryTransitions = 0
}

func buildRecoverSuccessHint(lastSummary string, lastExecFeedback *assistExecutionFeedback) string {
	parts := []string{"recovery mode active: reason from the latest command result before rereading older inputs; complete if evidence is sufficient"}
	if trimmed := strings.TrimSpace(lastSummary); trimmed != "" {
		parts = append(parts, "latest_result="+singleLine(trimmed, 220))
	} else if lastExecFeedback != nil && strings.TrimSpace(lastExecFeedback.ResultSummary) != "" {
		parts = append(parts, "latest_result="+singleLine(lastExecFeedback.ResultSummary, 220))
	}
	if lastExecFeedback != nil {
		refs := buildPrimaryEvidenceRefs(*lastExecFeedback)
		if len(refs) > 0 {
			parts = append(parts, "prefer_primary_evidence="+strings.Join(refs, ", "))
		}
	}
	return strings.Join(parts, " | ")
}
