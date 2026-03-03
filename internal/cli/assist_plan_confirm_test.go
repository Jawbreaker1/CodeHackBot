package cli

import (
	"strings"
	"testing"
)

func TestShouldRequirePlanConfirmation(t *testing.T) {
	if shouldRequirePlanConfirmation(nil) {
		t.Fatalf("expected no confirmation when there are no executable steps")
	}
	if !shouldRequirePlanConfirmation([]string{"step one"}) {
		t.Fatalf("expected confirmation when plan has executable steps")
	}
}

func TestComplexPlanConfirmationReasonsStepCount(t *testing.T) {
	reasons := complexPlanConfirmationReasons(
		"extract secret.zip",
		[]string{
			"check working directory",
			"locate archive",
			"extract hash",
			"attempt cracking",
		},
		"",
	)
	if len(reasons) == 0 {
		t.Fatalf("expected step-count reason")
	}
	if !strings.Contains(strings.ToLower(strings.Join(reasons, " | ")), "planned steps") {
		t.Fatalf("expected planned-steps reason, got %v", reasons)
	}
}

func TestComplexPlanConfirmationReasonsNetworkExpansion(t *testing.T) {
	reasons := complexPlanConfirmationReasons(
		"check host 192.168.50.185 only",
		[]string{
			"run host discovery on 192.168.50.0/24",
			"scan candidate host",
		},
		"",
	)
	joined := strings.ToLower(strings.Join(reasons, " | "))
	if !strings.Contains(joined, "subnet/network-wide scope") {
		t.Fatalf("expected network expansion reason, got %v", reasons)
	}
}

func TestComplexPlanConfirmationReasonsBruteforce(t *testing.T) {
	reasons := complexPlanConfirmationReasons(
		"open encrypted archive",
		[]string{
			"generate hash from zip",
			"run wordlist brute force attack",
		},
		"",
	)
	joined := strings.ToLower(strings.Join(reasons, " | "))
	if !strings.Contains(joined, "password-guessing") {
		t.Fatalf("expected password-guessing reason, got %v", reasons)
	}
}

func TestComplexPlanConfirmationReasonsNoTriggerForSimplePlan(t *testing.T) {
	reasons := complexPlanConfirmationReasons(
		"read secret.zip metadata",
		[]string{
			"list current directory",
			"run zipinfo secret.zip",
		},
		"",
	)
	if len(reasons) != 0 {
		t.Fatalf("expected no reasons for small/simple plan, got %v", reasons)
	}
}
