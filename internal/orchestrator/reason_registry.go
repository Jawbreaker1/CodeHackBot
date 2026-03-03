package orchestrator

import "strings"

const (
	TaskFailureReasonWorkerExit               = "worker_exit"
	TaskFailureReasonWorkerReconcileStale     = "worker_reconcile_stale"
	TaskFailureReasonMissingRequiredArtifacts = "missing_required_artifacts"
	TaskFailureReasonMissingPrereq            = "missing_prereq"
	TaskFailureReasonStartupSLAMissed         = "startup_sla_missed"
	TaskFailureReasonStaleLease               = "stale_lease"
	TaskFailureReasonLaunchFailed             = "launch_failed"
	TaskFailureReasonApprovalTimeout          = "approval_timeout"
	TaskFailureReasonApprovalDenied           = "approval_denied"
	TaskFailureReasonExecutionTimeout         = "execution_timeout"
	TaskFailureReasonBudgetExhausted          = "budget_exhausted"
	TaskFailureReasonRepeatedStepLoop         = "repeated_step_loop"
)

var knownTaskFailureReasons = map[string]struct{}{
	WorkerFailureInvalidTaskAction:    {},
	WorkerFailureWorkspaceCreate:      {},
	WorkerFailureInvalidWorkingDir:    {},
	WorkerFailureWorkingDirCreate:     {},
	WorkerFailureArtifactWrite:        {},
	WorkerFailureCommandFailed:        {},
	WorkerFailureCommandTimeout:       {},
	WorkerFailureCommandInterrupted:   {},
	WorkerFailureScopeDenied:          {},
	WorkerFailurePolicyDenied:         {},
	WorkerFailurePolicyInvalid:        {},
	WorkerFailureBootstrapFailed:      {},
	WorkerFailureInsufficientEvidence: {},
	WorkerFailureAssistUnavailable:    {},
	WorkerFailureAssistParseFailure:   {},
	WorkerFailureAssistTimeout:        {},
	WorkerFailureAssistNeedsInput:     {},
	WorkerFailureAssistNoAction:       {},
	WorkerFailureAssistLoopDetected:   {},
	WorkerFailureAssistExhausted:      {},
	WorkerFailureNoProgress:           {},

	TaskFailureReasonWorkerExit:               {},
	TaskFailureReasonWorkerReconcileStale:     {},
	TaskFailureReasonMissingRequiredArtifacts: {},
	TaskFailureReasonMissingPrereq:            {},
	TaskFailureReasonStartupSLAMissed:         {},
	TaskFailureReasonStaleLease:               {},
	TaskFailureReasonLaunchFailed:             {},
	TaskFailureReasonApprovalTimeout:          {},
	TaskFailureReasonApprovalDenied:           {},
	TaskFailureReasonExecutionTimeout:         {},
	TaskFailureReasonBudgetExhausted:          {},
	TaskFailureReasonRepeatedStepLoop:         {},
}

func NormalizeTaskFailureReason(raw string) string {
	reason := strings.ToLower(strings.TrimSpace(raw))
	if _, ok := knownTaskFailureReasons[reason]; ok {
		return reason
	}
	return ""
}

func CanonicalTaskFailureReason(raw string) string {
	if normalized := NormalizeTaskFailureReason(raw); normalized != "" {
		return normalized
	}
	return strings.ToLower(strings.TrimSpace(raw))
}
