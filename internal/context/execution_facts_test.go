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

	assertExecutionFact(t, facts, ExecutionFactKindCurrentTarget, "secret.zip", "selected")
	assertExecutionFact(t, facts, ExecutionFactKindUnresolvedMissingFact, "correct path or artifact needed: /usr/share/wordlists/rockyou.txt", "unresolved")
	assertExecutionFact(t, facts, ExecutionFactKindLatestExecutionStatus, "john secret.zip.hash", "failed")
	assertExecutionFact(t, facts, ExecutionFactKindRecoverySemantic, RecoverySemanticMissingFileOrPath, RecoveryStatusEstablishMissingPrerequisite)
	assertExecutionFact(t, facts, ExecutionFactKindExecutionSignal, "missing_path", "observed")
	assertExecutionFact(t, facts, ExecutionFactKindLogRef, "logs/cmd-1.log", "recorded")
	assertExecutionFact(t, facts, ExecutionFactKindArtifactRef, "artifacts/secret.zip.hash", "available")
}

func TestUpdateExecutionFactsClearsVolatileMissingFactButRetainsArtifacts(t *testing.T) {
	current := []ExecutionFact{
		{Kind: ExecutionFactKindUnresolvedMissingFact, Subject: "credential required", Status: "unresolved", Source: "task_runtime.missing_fact"},
		{Kind: ExecutionFactKindArtifactRef, Subject: "artifacts/secret.zip.hash", Status: "available", Source: "latest_execution_result.artifact_refs"},
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

	if hasFact(facts, ExecutionFactKindUnresolvedMissingFact, "credential required") {
		t.Fatalf("stale unresolved_missing_fact retained: %#v", facts)
	}
	assertExecutionFact(t, facts, ExecutionFactKindArtifactRef, "artifacts/secret.zip.hash", "available")
	assertExecutionFact(t, facts, ExecutionFactKindLatestExecutionStatus, "unzip -P password secret.zip", "success")
}

func TestRecoverySemanticDerivedFromStructuredResultState(t *testing.T) {
	cases := []struct {
		name        string
		result      ExecutionResult
		wantSubject string
		wantStatus  string
	}{
		{
			name: "missing path",
			result: ExecutionResult{
				Action:  "cat /missing/file",
				Signals: []string{"missing_path"},
			},
			wantSubject: RecoverySemanticMissingFileOrPath,
			wantStatus:  RecoveryStatusEstablishMissingPrerequisite,
		},
		{
			name: "missing tool",
			result: ExecutionResult{
				Action:  "madeuptool --version",
				Signals: []string{"not_executable"},
			},
			wantSubject: RecoverySemanticMissingToolOrCommand,
			wantStatus:  RecoveryStatusEstablishOrChooseAvailableTool,
		},
		{
			name: "timeout",
			result: ExecutionResult{
				Action:       "nmap -sV --top-ports 1000 127.0.0.1",
				FailureClass: "execution_interrupted",
				Signals:      []string{"execution_timeout"},
			},
			wantSubject: RecoverySemanticExecutionInterrupted,
			wantStatus:  RecoveryStatusRetryWithTighterBoundsOrRequestRun,
		},
		{
			name: "nonzero with useful output",
			result: ExecutionResult{
				Action:         "nmap 127.0.0.1",
				ExitStatus:     "1",
				OutputEvidence: "Host is up",
				Assessment:     "failed",
			},
			wantSubject: RecoverySemanticNonzeroWithUsefulOutput,
			wantStatus:  RecoveryStatusInterpretEvidenceOrReviseAction,
		},
		{
			name: "empty output",
			result: ExecutionResult{
				Action:  "grep needle file",
				Signals: []string{"empty_output"},
			},
			wantSubject: RecoverySemanticNoOutputOrNoEffect,
			wantStatus:  RecoveryStatusChooseEvidenceProducingAction,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subject, status, source := recoverySemantic(tc.result)
			if subject != tc.wantSubject || status != tc.wantStatus {
				t.Fatalf("recoverySemantic() = (%q, %q, %q), want subject=%q status=%q", subject, status, source, tc.wantSubject, tc.wantStatus)
			}
			if source == "" {
				t.Fatalf("recoverySemantic() source is empty")
			}
		})
	}
}

func TestRecoverySemanticNotDerivedWithoutRecoveryNeed(t *testing.T) {
	subject, status, source := recoverySemantic(ExecutionResult{
		Action:     "pwd",
		ExitStatus: "0",
		Assessment: "success",
	})
	if subject != "" || status != "" || source != "" {
		t.Fatalf("recoverySemantic() = (%q, %q, %q), want empty", subject, status, source)
	}
}

func TestUpdateExecutionFactsDedupesAndCaps(t *testing.T) {
	current := make([]ExecutionFact, 0, executionFactLimit+4)
	for i := 0; i < executionFactLimit+4; i++ {
		current = append(current, ExecutionFact{
			Kind:    ExecutionFactKindArtifactRef,
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
			Kind:    ExecutionFactKindArtifactRef,
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
