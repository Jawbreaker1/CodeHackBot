package context

import "strings"

const executionFactLimit = 12

const (
	ExecutionFactKindCurrentTarget         = "current_target"
	ExecutionFactKindUnresolvedMissingFact = "unresolved_missing_fact"
	ExecutionFactKindLatestExecutionStatus = "latest_execution_status"
	ExecutionFactKindExecutionSignal       = "execution_signal"
	ExecutionFactKindRecoverySemantic      = "recovery_semantic"
	ExecutionFactKindLogRef                = "log_ref"
	ExecutionFactKindArtifactRef           = "artifact_ref"
)

const (
	RecoverySemanticMissingFileOrPath       = "missing_file_or_path"
	RecoverySemanticMissingToolOrCommand    = "missing_tool_or_command"
	RecoverySemanticPermissionDenied        = "permission_denied"
	RecoverySemanticMissingCredential       = "missing_credential"
	RecoverySemanticExecutionInterrupted    = "execution_interrupted"
	RecoverySemanticNoOutputOrNoEffect      = "no_output_or_no_effect"
	RecoverySemanticNonzeroWithUsefulOutput = "nonzero_with_useful_output"
	RecoverySemanticGenericFailure          = "generic_execution_failure"
)

const (
	RecoveryStatusEstablishMissingPrerequisite       = "establish_missing_prerequisite"
	RecoveryStatusEstablishOrChooseAvailableTool     = "establish_or_choose_available_tool"
	RecoveryStatusChangePermissionScopeOrTarget      = "change_permission_scope_or_target"
	RecoveryStatusRetryWithTighterBoundsOrRequestRun = "retry_with_tighter_bounds_or_request_deeper_run"
	RecoveryStatusChooseEvidenceProducingAction      = "choose_evidence_producing_action"
	RecoveryStatusInterpretEvidenceOrReviseAction    = "interpret_evidence_or_revise_action"
	RecoveryStatusReviseActionOrEstablishPrereq      = "revise_action_or_establish_prerequisite"
)

const (
	RecoverySourceLatestResult             = "latest_execution_result"
	RecoverySourceLatestResultSignals      = "latest_execution_result.signals"
	RecoverySourceLatestResultFailureClass = "latest_execution_result.failure_class"
)

// UpdateExecutionFacts returns a small active slice of facts derived from
// structured runtime truth. It intentionally does not parse raw command output.
func UpdateExecutionFacts(current []ExecutionFact, runtime TaskRuntime, latest ExecutionResult) []ExecutionFact {
	fresh := make([]ExecutionFact, 0, 8)
	if target := strings.TrimSpace(runtime.CurrentTarget); target != "" {
		fresh = append(fresh, ExecutionFact{
			Kind:    ExecutionFactKindCurrentTarget,
			Subject: target,
			Status:  "selected",
			Source:  "task_runtime.current_target",
		})
	}
	if missing := strings.TrimSpace(runtime.MissingFact); missing != "" && missing != "(none)" {
		fresh = append(fresh, ExecutionFact{
			Kind:         ExecutionFactKindUnresolvedMissingFact,
			Subject:      missing,
			Status:       "unresolved",
			Source:       "task_runtime.missing_fact",
			EvidenceRefs: cloneStrings(latest.LogRefs),
		})
	}
	if strings.TrimSpace(latest.Action) != "" {
		fresh = append(fresh, ExecutionFact{
			Kind:         ExecutionFactKindLatestExecutionStatus,
			Subject:      latest.Action,
			Status:       executionStatus(latest),
			Source:       "latest_execution_result",
			EvidenceRefs: cloneStrings(latest.LogRefs),
		})
		if fact, ok := recoverySemanticFact(latest); ok {
			fresh = append(fresh, fact)
		}
		for _, signal := range latest.Signals {
			if signal = strings.TrimSpace(signal); signal != "" {
				fresh = append(fresh, ExecutionFact{
					Kind:         ExecutionFactKindExecutionSignal,
					Subject:      signal,
					Status:       "observed",
					Source:       "latest_execution_result.signals",
					EvidenceRefs: cloneStrings(latest.LogRefs),
				})
			}
		}
		for _, ref := range latest.LogRefs {
			if ref = strings.TrimSpace(ref); ref != "" {
				fresh = append(fresh, ExecutionFact{
					Kind:         ExecutionFactKindLogRef,
					Subject:      ref,
					Status:       "recorded",
					Source:       "latest_execution_result.log_refs",
					EvidenceRefs: []string{ref},
				})
			}
		}
		for _, ref := range latest.ArtifactRefs {
			if ref = strings.TrimSpace(ref); ref != "" {
				fresh = append(fresh, ExecutionFact{
					Kind:         ExecutionFactKindArtifactRef,
					Subject:      ref,
					Status:       "available",
					Source:       "latest_execution_result.artifact_refs",
					EvidenceRefs: cloneStrings(latest.LogRefs),
				})
			}
		}
	}

	retained := make([]ExecutionFact, 0, len(current))
	for _, fact := range current {
		if isVolatileExecutionFact(fact) {
			continue
		}
		retained = append(retained, fact)
	}
	return mergeExecutionFacts(append(fresh, retained...))
}

func executionStatus(result ExecutionResult) string {
	if failure := strings.TrimSpace(result.FailureClass); failure != "" {
		return "failed:" + failure
	}
	if assessment := strings.TrimSpace(result.Assessment); assessment != "" {
		return assessment
	}
	if exit := strings.TrimSpace(result.ExitStatus); exit != "" {
		return "exit:" + exit
	}
	return "observed"
}

func isVolatileExecutionFact(fact ExecutionFact) bool {
	switch strings.TrimSpace(fact.Kind) {
	case ExecutionFactKindCurrentTarget, ExecutionFactKindUnresolvedMissingFact, ExecutionFactKindLatestExecutionStatus, ExecutionFactKindExecutionSignal, ExecutionFactKindRecoverySemantic:
		return true
	default:
		return false
	}
}

func recoverySemanticFact(result ExecutionResult) (ExecutionFact, bool) {
	subject, status, source := recoverySemantic(result)
	if subject == "" {
		return ExecutionFact{}, false
	}
	return ExecutionFact{
		Kind:         ExecutionFactKindRecoverySemantic,
		Subject:      subject,
		Status:       status,
		Source:       source,
		EvidenceRefs: cloneStrings(result.LogRefs),
	}, true
}

func recoverySemantic(result ExecutionResult) (subject, status, source string) {
	if strings.TrimSpace(result.Action) == "" {
		return "", "", ""
	}
	switch {
	case hasSignal(result.Signals, "missing_path"):
		return RecoverySemanticMissingFileOrPath, RecoveryStatusEstablishMissingPrerequisite, RecoverySourceLatestResultSignals
	case hasAnySignal(result.Signals, "not_executable", "command_not_found"):
		return RecoverySemanticMissingToolOrCommand, RecoveryStatusEstablishOrChooseAvailableTool, RecoverySourceLatestResultSignals
	case hasSignal(result.Signals, "permission_denied"):
		return RecoverySemanticPermissionDenied, RecoveryStatusChangePermissionScopeOrTarget, RecoverySourceLatestResultSignals
	case hasSignal(result.Signals, "incorrect_password"):
		return RecoverySemanticMissingCredential, RecoveryStatusEstablishMissingPrerequisite, RecoverySourceLatestResultSignals
	case IsInterruptedResult(result):
		return RecoverySemanticExecutionInterrupted, RecoveryStatusRetryWithTighterBoundsOrRequestRun, recoverySource(result)
	case hasAnySignal(result.Signals, "empty_output", "no_effect"):
		return RecoverySemanticNoOutputOrNoEffect, RecoveryStatusChooseEvidenceProducingAction, RecoverySourceLatestResultSignals
	case nonzeroExit(result.ExitStatus) && hasUsefulExecutionOutput(result):
		return RecoverySemanticNonzeroWithUsefulOutput, RecoveryStatusInterpretEvidenceOrReviseAction, RecoverySourceLatestResult
	case nonzeroExit(result.ExitStatus) || strings.TrimSpace(result.FailureClass) != "" || strings.TrimSpace(result.Assessment) == "failed":
		return RecoverySemanticGenericFailure, RecoveryStatusReviseActionOrEstablishPrereq, recoverySource(result)
	default:
		return "", "", ""
	}
}

func recoverySource(result ExecutionResult) string {
	if strings.TrimSpace(result.FailureClass) != "" {
		return RecoverySourceLatestResultFailureClass
	}
	if len(result.Signals) > 0 {
		return RecoverySourceLatestResultSignals
	}
	return RecoverySourceLatestResult
}

func hasAnySignal(signals []string, wants ...string) bool {
	for _, want := range wants {
		if hasSignal(signals, want) {
			return true
		}
	}
	return false
}

func hasUsefulExecutionOutput(result ExecutionResult) bool {
	return meaningfulExecutionText(result.OutputEvidence) || meaningfulExecutionText(result.OutputSummary)
}

func meaningfulExecutionText(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && text != "(none)"
}

func mergeExecutionFacts(facts []ExecutionFact) []ExecutionFact {
	seen := make(map[string]struct{}, len(facts))
	out := make([]ExecutionFact, 0, min(executionFactLimit, len(facts)))
	for _, fact := range facts {
		fact = normalizeExecutionFact(fact)
		if fact.Kind == "" || fact.Subject == "" {
			continue
		}
		key := strings.Join([]string{fact.Kind, fact.Subject, fact.Status}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, fact)
		if len(out) >= executionFactLimit {
			break
		}
	}
	return out
}

func normalizeExecutionFact(fact ExecutionFact) ExecutionFact {
	fact.Kind = strings.TrimSpace(fact.Kind)
	fact.Subject = strings.TrimSpace(fact.Subject)
	fact.Status = strings.TrimSpace(fact.Status)
	fact.Source = strings.TrimSpace(fact.Source)
	fact.EvidenceRefs = compactStrings(fact.EvidenceRefs)
	return fact
}

func compactStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	return append([]string(nil), items...)
}
