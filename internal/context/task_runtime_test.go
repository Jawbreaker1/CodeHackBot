package context

import (
	"testing"
)

func TestInitialTaskRuntimeInfersTarget(t *testing.T) {
	got := InitialTaskRuntime("Extract contents of secret.zip and identify the password needed to decrypt it.")
	if got.State != "running" {
		t.Fatalf("State = %q", got.State)
	}
	if got.CurrentTarget != "secret.zip" {
		t.Fatalf("CurrentTarget = %q", got.CurrentTarget)
	}
}

func TestUpdateTaskRuntimeUsesSignals(t *testing.T) {
	got := UpdateTaskRuntime(
		TaskRuntime{State: "running", CurrentTarget: "secret.zip"},
		"Extract secret.zip",
		ExecutionResult{
			Action:        "unzip -t secret.zip",
			ExitStatus:    "0",
			OutputSummary: "stdout: unable to get password",
			Assessment:    "suspicious",
			Signals:       []string{"incorrect_password"},
		},
	)
	if got.MissingFact != "credential or password required for secret.zip" {
		t.Fatalf("MissingFact = %q", got.MissingFact)
	}
}

func TestUpdateTaskRuntimeDefaultsToNextEvidenceAboutTarget(t *testing.T) {
	got := UpdateTaskRuntime(
		TaskRuntime{State: "running", CurrentTarget: "127.0.0.1"},
		"Perform local reconnaissance of 127.0.0.1",
		ExecutionResult{
			Action:        "nmap -sV 127.0.0.1",
			ExitStatus:    "0",
			OutputSummary: "stdout: 22/tcp open ssh OpenSSH 10.2p1 Debian 3",
			Assessment:    "success",
		},
	)
	if got.MissingFact != "next evidence needed about 127.0.0.1" {
		t.Fatalf("MissingFact = %q", got.MissingFact)
	}
}
