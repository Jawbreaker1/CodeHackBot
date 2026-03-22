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
