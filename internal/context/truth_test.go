package context

import "testing"

func TestActiveTruthResultPrefersRetainedSuspiciousResultOverLatestSuccess(t *testing.T) {
	latest := ExecutionResult{
		Action:        "find /home/johan -name \"secret.zip\"",
		ExitStatus:    "0",
		OutputSummary: "found many files",
		Assessment:    "success",
	}
	recent := []ExecutionResult{
		{
			Action:        "unzip -t secret.zip",
			ExitStatus:    "0",
			OutputSummary: "unable to get password",
			Assessment:    "suspicious",
			Signals:       []string{"incorrect_password"},
		},
	}

	got := ActiveTruthResult(latest, recent)
	if got.Action != "unzip -t secret.zip" {
		t.Fatalf("ActiveTruthResult().Action = %q", got.Action)
	}
}

func TestActiveTruthResultKeepsLatestWhenItIsStrongest(t *testing.T) {
	latest := ExecutionResult{
		Action:        "fcrackzip ...",
		ExitStatus:    "0",
		OutputSummary: "missing path",
		Assessment:    "suspicious",
		Signals:       []string{"missing_path"},
	}
	recent := []ExecutionResult{
		{
			Action:        "ls -la secret.zip",
			ExitStatus:    "0",
			OutputSummary: "secret.zip present",
			Assessment:    "success",
		},
	}

	got := ActiveTruthResult(latest, recent)
	if got.Action != latest.Action {
		t.Fatalf("ActiveTruthResult().Action = %q, want %q", got.Action, latest.Action)
	}
}

func TestActiveTruthResultPrefersIncorrectPasswordOverLaterMissingPath(t *testing.T) {
	latest := ExecutionResult{
		Action:        "fcrackzip -D -p /bad/path secret.zip",
		ExitStatus:    "0",
		OutputSummary: "stdout: missing path",
		Assessment:    "suspicious",
		Signals:       []string{"missing_path"},
	}
	recent := []ExecutionResult{
		{
			Action:        "unzip -t secret.zip",
			ExitStatus:    "0",
			OutputSummary: "stdout: unable to get password",
			Assessment:    "suspicious",
			Signals:       []string{"incorrect_password"},
		},
	}

	got := ActiveTruthResult(latest, recent)
	if got.Action != "unzip -t secret.zip" {
		t.Fatalf("ActiveTruthResult().Action = %q", got.Action)
	}
}

func TestImpactOfResultBlocking(t *testing.T) {
	got := ImpactOfResult(ExecutionResult{
		Action:     "fcrackzip ...",
		Assessment: "suspicious",
		Signals:    []string{"missing_path"},
	})
	if got != ResultImpactBlocking {
		t.Fatalf("ImpactOfResult() = %q, want %q", got, ResultImpactBlocking)
	}
}

func TestImpactOfResultRecoverable(t *testing.T) {
	got := ImpactOfResult(ExecutionResult{
		Action:       "nmap ...",
		ExitStatus:   "1",
		Assessment:   "failed",
		FailureClass: "command_failed",
	})
	if got != ResultImpactRecoverable {
		t.Fatalf("ImpactOfResult() = %q, want %q", got, ResultImpactRecoverable)
	}
}

func TestImpactOfInterruptedResultRecoverable(t *testing.T) {
	got := ImpactOfResult(ExecutionResult{
		Action:       "nmap -sV --top-ports 1000 192.168.50.1",
		ExitStatus:   "-1",
		Assessment:   "ambiguous",
		FailureClass: "execution_interrupted",
		Signals:      []string{"execution_timeout"},
	})
	if got != ResultImpactRecoverable {
		t.Fatalf("ImpactOfResult() = %q, want %q", got, ResultImpactRecoverable)
	}
}

func TestIsInterruptedResult(t *testing.T) {
	if !IsInterruptedResult(ExecutionResult{
		Action:       "nmap ...",
		FailureClass: "execution_interrupted",
	}) {
		t.Fatal("expected interrupted result from failure class")
	}
	if !IsInterruptedResult(ExecutionResult{
		Action:  "nmap ...",
		Signals: []string{"execution_timeout"},
	}) {
		t.Fatal("expected interrupted result from signal")
	}
}

func TestImpactOfResultInformational(t *testing.T) {
	got := ImpactOfResult(ExecutionResult{
		Action:     "unzip -l secret.zip",
		ExitStatus: "0",
		Assessment: "success",
	})
	if got != ResultImpactInformational {
		t.Fatalf("ImpactOfResult() = %q, want %q", got, ResultImpactInformational)
	}
}
