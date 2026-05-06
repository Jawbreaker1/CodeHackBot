package context

import "strings"

const executionFactLimit = 12

// UpdateExecutionFacts returns a small active slice of facts derived from
// structured runtime truth. It intentionally does not parse raw command output.
func UpdateExecutionFacts(current []ExecutionFact, runtime TaskRuntime, latest ExecutionResult) []ExecutionFact {
	fresh := make([]ExecutionFact, 0, 8)
	if target := strings.TrimSpace(runtime.CurrentTarget); target != "" {
		fresh = append(fresh, ExecutionFact{
			Kind:    "current_target",
			Subject: target,
			Status:  "selected",
			Source:  "task_runtime.current_target",
		})
	}
	if missing := strings.TrimSpace(runtime.MissingFact); missing != "" && missing != "(none)" {
		fresh = append(fresh, ExecutionFact{
			Kind:         "unresolved_missing_fact",
			Subject:      missing,
			Status:       "unresolved",
			Source:       "task_runtime.missing_fact",
			EvidenceRefs: cloneStrings(latest.LogRefs),
		})
	}
	if strings.TrimSpace(latest.Action) != "" {
		fresh = append(fresh, ExecutionFact{
			Kind:         "latest_execution_status",
			Subject:      latest.Action,
			Status:       executionStatus(latest),
			Source:       "latest_execution_result",
			EvidenceRefs: cloneStrings(latest.LogRefs),
		})
		for _, signal := range latest.Signals {
			if signal = strings.TrimSpace(signal); signal != "" {
				fresh = append(fresh, ExecutionFact{
					Kind:         "execution_signal",
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
					Kind:         "log_ref",
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
					Kind:         "artifact_ref",
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
	case "current_target", "unresolved_missing_fact", "latest_execution_status", "execution_signal":
		return true
	default:
		return false
	}
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
