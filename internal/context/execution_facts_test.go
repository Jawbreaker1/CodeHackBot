package context

import "testing"

func TestUpdateExecutionFactsDerivesFactsFromStructuredTruth(t *testing.T) {
	facts := UpdateExecutionFacts(nil,
		TaskRuntime{
			State:         "running",
			CurrentTarget: "secret.zip",
			MissingFact:   "correct path or artifact needed: /usr/share/wordlists/rockyou.txt",
		},
		ExecutionResult{
			Action:       "john secret.zip.hash",
			ExitStatus:   "1",
			LogRefs:      []string{"logs/cmd-1.log"},
			ArtifactRefs: []string{"artifacts/secret.zip.hash"},
			Assessment:   "failed",
			Signals:      []string{"missing_path"},
		},
	)

	assertExecutionFact(t, facts, "current_target", "secret.zip", "selected")
	assertExecutionFact(t, facts, "unresolved_missing_fact", "correct path or artifact needed: /usr/share/wordlists/rockyou.txt", "unresolved")
	assertExecutionFact(t, facts, "latest_execution_status", "john secret.zip.hash", "failed")
	assertExecutionFact(t, facts, "execution_signal", "missing_path", "observed")
	assertExecutionFact(t, facts, "log_ref", "logs/cmd-1.log", "recorded")
	assertExecutionFact(t, facts, "artifact_ref", "artifacts/secret.zip.hash", "available")
}

func TestUpdateExecutionFactsClearsVolatileMissingFactButRetainsArtifacts(t *testing.T) {
	current := []ExecutionFact{
		{Kind: "unresolved_missing_fact", Subject: "credential required", Status: "unresolved", Source: "task_runtime.missing_fact"},
		{Kind: "artifact_ref", Subject: "artifacts/secret.zip.hash", Status: "available", Source: "latest_execution_result.artifact_refs"},
	}

	facts := UpdateExecutionFacts(current,
		TaskRuntime{
			State:         "done",
			CurrentTarget: "secret.zip",
			MissingFact:   "(none)",
		},
		ExecutionResult{
			Action:     "unzip -P password secret.zip",
			ExitStatus: "0",
			LogRefs:    []string{"logs/cmd-2.log"},
			Assessment: "success",
		},
	)

	if hasFact(facts, "unresolved_missing_fact", "credential required") {
		t.Fatalf("stale unresolved_missing_fact retained: %#v", facts)
	}
	assertExecutionFact(t, facts, "artifact_ref", "artifacts/secret.zip.hash", "available")
	assertExecutionFact(t, facts, "latest_execution_status", "unzip -P password secret.zip", "success")
}

func TestUpdateExecutionFactsDedupesAndCaps(t *testing.T) {
	current := make([]ExecutionFact, 0, executionFactLimit+4)
	for i := 0; i < executionFactLimit+4; i++ {
		current = append(current, ExecutionFact{
			Kind:    "artifact_ref",
			Subject: "artifacts/item.txt",
			Status:  "available",
			Source:  "test",
		})
	}

	facts := UpdateExecutionFacts(current, TaskRuntime{}, ExecutionResult{})
	if len(facts) != 1 {
		t.Fatalf("len(facts) = %d, want 1: %#v", len(facts), facts)
	}

	current = current[:0]
	for i := 0; i < executionFactLimit+4; i++ {
		current = append(current, ExecutionFact{
			Kind:    "artifact_ref",
			Subject: string(rune('a' + i)),
			Status:  "available",
			Source:  "test",
		})
	}
	facts = UpdateExecutionFacts(current, TaskRuntime{}, ExecutionResult{})
	if len(facts) != executionFactLimit {
		t.Fatalf("len(facts) = %d, want %d", len(facts), executionFactLimit)
	}
}

func assertExecutionFact(t *testing.T, facts []ExecutionFact, kind, subject, status string) {
	t.Helper()
	for _, fact := range facts {
		if fact.Kind == kind && fact.Subject == subject && fact.Status == status {
			return
		}
	}
	t.Fatalf("missing fact kind=%q subject=%q status=%q in %#v", kind, subject, status, facts)
}

func hasFact(facts []ExecutionFact, kind, subject string) bool {
	for _, fact := range facts {
		if fact.Kind == kind && fact.Subject == subject {
			return true
		}
	}
	return false
}
