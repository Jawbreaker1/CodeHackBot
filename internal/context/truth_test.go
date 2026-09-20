package context

import "testing"

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
