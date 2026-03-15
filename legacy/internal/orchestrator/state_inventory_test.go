package orchestrator

import (
	"regexp"
	"strings"
	"testing"
)

var enumTokenPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

func TestStateInventoryEventTypesUnique(t *testing.T) {
	events := CanonicalEventTypes()
	assertUniqueNonEmptyTokens(t, events, "event type")
}

func TestStateInventoryEventMutationDomainsUnique(t *testing.T) {
	domains := []string{
		string(MutationDomainTaskLifecycle),
		string(MutationDomainApprovalStatus),
		string(MutationDomainReplanGraph),
		string(MutationDomainRunProjection),
		string(MutationDomainTaskProjection),
		string(MutationDomainWorkerProjection),
	}
	assertUniqueNonEmptyTokens(t, domains, "event mutation domain")
}

func TestStateInventoryEventMutationTableCoverage(t *testing.T) {
	if err := ValidateEventMutationTable(); err != nil {
		t.Fatalf("ValidateEventMutationTable: %v", err)
	}
}

func TestStateInventoryTaskStatesValidate(t *testing.T) {
	states := []TaskState{
		TaskStateQueued,
		TaskStateLeased,
		TaskStateRunning,
		TaskStateAwaitingApproval,
		TaskStateCompleted,
		TaskStateFailed,
		TaskStateBlocked,
		TaskStateCanceled,
	}
	seen := map[TaskState]struct{}{}
	for _, state := range states {
		if _, ok := seen[state]; ok {
			t.Fatalf("duplicate task state %q", state)
		}
		seen[state] = struct{}{}
		if err := ValidateTaskState(state); err != nil {
			t.Fatalf("ValidateTaskState(%q): %v", state, err)
		}
		if !enumTokenPattern.MatchString(string(state)) {
			t.Fatalf("task state %q does not match token pattern", state)
		}
	}
}

func TestStateInventoryTaskTerminalStatesHaveNoOutgoingTransitions(t *testing.T) {
	terminals := []TaskState{TaskStateCompleted, TaskStateCanceled}
	for _, state := range terminals {
		next, ok := allowedTransitions[state]
		if !ok {
			t.Fatalf("missing transition map for terminal task state %q", state)
		}
		if len(next) != 0 {
			t.Fatalf("expected terminal state %q to have no outgoing transitions, got %d", state, len(next))
		}
	}
}

func TestStateInventoryLeaseStatusesUnique(t *testing.T) {
	statuses := []string{
		LeaseStatusLeased,
		LeaseStatusQueued,
		LeaseStatusAwaitingApproval,
		LeaseStatusRunning,
		LeaseStatusCompleted,
		LeaseStatusFailed,
		LeaseStatusBlocked,
		LeaseStatusCanceled,
	}
	assertUniqueNonEmptyTokens(t, statuses, "lease status")
}

func TestStateInventoryLeaseStatusesMirrorTaskStates(t *testing.T) {
	taskStates := map[string]struct{}{
		string(TaskStateQueued):           {},
		string(TaskStateLeased):           {},
		string(TaskStateRunning):          {},
		string(TaskStateAwaitingApproval): {},
		string(TaskStateCompleted):        {},
		string(TaskStateFailed):           {},
		string(TaskStateBlocked):          {},
		string(TaskStateCanceled):         {},
	}
	leaseStates := []string{
		LeaseStatusLeased,
		LeaseStatusQueued,
		LeaseStatusAwaitingApproval,
		LeaseStatusRunning,
		LeaseStatusCompleted,
		LeaseStatusFailed,
		LeaseStatusBlocked,
		LeaseStatusCanceled,
	}
	for _, leaseState := range leaseStates {
		if _, ok := taskStates[leaseState]; !ok {
			t.Fatalf("lease status %q is not mirrored by TaskState", leaseState)
		}
		delete(taskStates, leaseState)
	}
	if len(taskStates) != 0 {
		missing := make([]string, 0, len(taskStates))
		for state := range taskStates {
			missing = append(missing, state)
		}
		t.Fatalf("TaskState values missing from LeaseStatus mirror: %s", strings.Join(missing, ","))
	}
}

func TestStateInventoryRunPhasesValidate(t *testing.T) {
	phases := []string{
		RunPhasePlanning,
		RunPhaseReview,
		RunPhaseApproved,
		RunPhaseExecuting,
		RunPhaseCompleted,
	}
	assertUniqueNonEmptyTokens(t, phases, "run phase")
	for _, phase := range phases {
		if err := ValidateRunPhase(phase); err != nil {
			t.Fatalf("ValidateRunPhase(%q): %v", phase, err)
		}
	}
}

func TestStateInventoryRunOutcomesValidate(t *testing.T) {
	outcomes := []string{
		string(RunOutcomeSuccess),
		string(RunOutcomeFailed),
		string(RunOutcomeAborted),
		string(RunOutcomePartial),
	}
	assertUniqueNonEmptyTokens(t, outcomes, "run outcome")
	for _, outcome := range outcomes {
		if err := ValidateRunOutcome(outcome); err != nil {
			t.Fatalf("ValidateRunOutcome(%q): %v", outcome, err)
		}
		if _, err := ParseRunOutcome(outcome); err != nil {
			t.Fatalf("ParseRunOutcome(%q): %v", outcome, err)
		}
	}
}

func TestStateInventoryApprovalEnumsUnique(t *testing.T) {
	assertUniqueNonEmptyTokens(t, []string{
		string(ApprovalStatusPending),
		string(ApprovalStatusGranted),
		string(ApprovalStatusDenied),
		string(ApprovalStatusExpired),
	}, "approval status")
	assertUniqueNonEmptyTokens(t, []string{
		string(ApprovalAllow),
		string(ApprovalNeedsRequest),
		string(ApprovalDeny),
	}, "approval decision")
	assertUniqueNonEmptyTokens(t, []string{
		string(ApprovalScopeOnce),
		string(ApprovalScopeTask),
		string(ApprovalScopeSession),
	}, "approval scope")
	assertUniqueNonEmptyTokens(t, []string{
		string(PermissionReadonly),
		string(PermissionDefault),
		string(PermissionAll),
	}, "permission mode")
	riskTiers := []string{
		string(RiskReconReadonly),
		string(RiskActiveProbe),
		string(RiskExploitControlled),
		string(RiskPrivEsc),
		string(RiskDisruptive),
	}
	assertUniqueNonEmptyTokens(t, riskTiers, "risk tier")
	for _, tier := range riskTiers {
		if _, err := ParseRiskTier(tier); err != nil {
			t.Fatalf("ParseRiskTier(%q): %v", tier, err)
		}
	}
}

func TestStateInventoryApprovalDecisionAndStatusDoNotOverlap(t *testing.T) {
	statuses := map[string]struct{}{
		string(ApprovalStatusPending): {},
		string(ApprovalStatusGranted): {},
		string(ApprovalStatusDenied):  {},
		string(ApprovalStatusExpired): {},
	}
	decisions := []string{
		string(ApprovalAllow),
		string(ApprovalNeedsRequest),
		string(ApprovalDeny),
	}
	for _, decision := range decisions {
		if _, ok := statuses[decision]; ok {
			t.Fatalf("approval decision %q overlaps approval status domain", decision)
		}
	}
}

func TestStateInventoryFindingAndReplanEnumsUnique(t *testing.T) {
	assertUniqueNonEmptyTokens(t, []string{
		FindingStateHypothesis,
		FindingStateCandidate,
		FindingStateVerified,
		FindingStateRejected,
	}, "finding state")
	assertUniqueNonEmptyTokens(t, []string{
		string(ReplanOutcomeRefineTask),
		string(ReplanOutcomeSplitTask),
		string(ReplanOutcomeTerminate),
	}, "replan outcome")
}

func TestStateInventoryWorkerFailureReasonsUnique(t *testing.T) {
	reasons := []string{
		WorkerFailureInvalidTaskAction,
		WorkerFailureWorkspaceCreate,
		WorkerFailureInvalidWorkingDir,
		WorkerFailureWorkingDirCreate,
		WorkerFailureArtifactWrite,
		WorkerFailureCommandFailed,
		WorkerFailureCommandTimeout,
		WorkerFailureCommandInterrupted,
		WorkerFailureScopeDenied,
		WorkerFailurePolicyDenied,
		WorkerFailurePolicyInvalid,
		WorkerFailureBootstrapFailed,
		WorkerFailureInsufficientEvidence,
		WorkerFailureAssistUnavailable,
		WorkerFailureAssistParseFailure,
		WorkerFailureAssistTimeout,
		WorkerFailureAssistNeedsInput,
		WorkerFailureAssistNoAction,
		WorkerFailureAssistLoopDetected,
		WorkerFailureAssistExhausted,
		WorkerFailureNoProgress,
	}
	assertUniqueNonEmptyTokens(t, reasons, "worker failure reason")
}

func TestStateInventoryWorkerFailureClassesUnique(t *testing.T) {
	assertUniqueNonEmptyTokens(t, []string{
		workerFailureClassContextLoss,
		workerFailureClassStrategy,
		workerFailureClassContract,
	}, "worker failure class")
}

func TestStateInventoryWorkerFailureReasonsClassified(t *testing.T) {
	reasons := []string{
		WorkerFailureInvalidTaskAction,
		WorkerFailureWorkspaceCreate,
		WorkerFailureInvalidWorkingDir,
		WorkerFailureWorkingDirCreate,
		WorkerFailureArtifactWrite,
		WorkerFailureCommandFailed,
		WorkerFailureCommandTimeout,
		WorkerFailureCommandInterrupted,
		WorkerFailureScopeDenied,
		WorkerFailurePolicyDenied,
		WorkerFailurePolicyInvalid,
		WorkerFailureBootstrapFailed,
		WorkerFailureInsufficientEvidence,
		WorkerFailureAssistUnavailable,
		WorkerFailureAssistParseFailure,
		WorkerFailureAssistTimeout,
		WorkerFailureAssistNeedsInput,
		WorkerFailureAssistNoAction,
		WorkerFailureAssistLoopDetected,
		WorkerFailureAssistExhausted,
		WorkerFailureNoProgress,
	}
	allowedClasses := map[string]struct{}{
		workerFailureClassContextLoss: {},
		workerFailureClassStrategy:    {},
		workerFailureClassContract:    {},
	}
	for _, reason := range reasons {
		class := classifyWorkerFailureReason(reason)
		if _, ok := allowedClasses[class]; !ok {
			t.Fatalf("reason %q mapped to invalid failure class %q", reason, class)
		}
	}
}

func TestStateInventoryTaskFailureReasonRegistryKnownAndUnique(t *testing.T) {
	if len(knownTaskFailureReasons) == 0 {
		t.Fatalf("knownTaskFailureReasons must not be empty")
	}
	seen := map[string]struct{}{}
	for reason := range knownTaskFailureReasons {
		if reason == "" {
			t.Fatalf("knownTaskFailureReasons contains empty reason")
		}
		if !enumTokenPattern.MatchString(reason) {
			t.Fatalf("task failure reason %q does not match token pattern", reason)
		}
		if _, ok := seen[reason]; ok {
			t.Fatalf("duplicate task failure reason %q", reason)
		}
		seen[reason] = struct{}{}
		if normalized := NormalizeTaskFailureReason(reason); normalized != reason {
			t.Fatalf("NormalizeTaskFailureReason(%q)=%q", reason, normalized)
		}
	}
}

func TestCanonicalTaskFailureReason_NormalizesKnownTokens(t *testing.T) {
	if got := CanonicalTaskFailureReason("  APPROVAL_TIMEOUT  "); got != TaskFailureReasonApprovalTimeout {
		t.Fatalf("CanonicalTaskFailureReason approval_timeout=%q", got)
	}
	if got := CanonicalTaskFailureReason("  worker_exit "); got != TaskFailureReasonWorkerExit {
		t.Fatalf("CanonicalTaskFailureReason worker_exit=%q", got)
	}
	if got := CanonicalTaskFailureReason("unknown_reason"); got != "unknown_reason" {
		t.Fatalf("CanonicalTaskFailureReason unknown=%q", got)
	}
}

func assertUniqueNonEmptyTokens(t *testing.T, values []string, label string) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, raw := range values {
		if raw == "" {
			t.Fatalf("%s contains empty value", label)
		}
		if !enumTokenPattern.MatchString(raw) {
			t.Fatalf("%s %q does not match token pattern", label, raw)
		}
		if _, ok := seen[raw]; ok {
			t.Fatalf("duplicate %s %q", label, raw)
		}
		seen[raw] = struct{}{}
	}
}
