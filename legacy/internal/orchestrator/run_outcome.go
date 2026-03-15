package orchestrator

import (
	"fmt"
	"strings"
)

type RunOutcome string

const (
	RunOutcomeSuccess RunOutcome = "success"
	RunOutcomeFailed  RunOutcome = "failed"
	RunOutcomeAborted RunOutcome = "aborted"
	RunOutcomePartial RunOutcome = "partial"
)

var validRunOutcomes = map[RunOutcome]struct{}{
	RunOutcomeSuccess: {},
	RunOutcomeFailed:  {},
	RunOutcomeAborted: {},
	RunOutcomePartial: {},
}

func NormalizeRunOutcome(raw string) RunOutcome {
	outcome := RunOutcome(strings.ToLower(strings.TrimSpace(raw)))
	if _, ok := validRunOutcomes[outcome]; ok {
		return outcome
	}
	return ""
}

func ValidateRunOutcome(raw string) error {
	if NormalizeRunOutcome(raw) == "" {
		return fmt.Errorf("invalid run outcome %q", raw)
	}
	return nil
}

func ParseRunOutcome(raw string) (RunOutcome, error) {
	outcome := NormalizeRunOutcome(raw)
	if outcome == "" {
		return "", fmt.Errorf("invalid run outcome %q", raw)
	}
	return outcome, nil
}
