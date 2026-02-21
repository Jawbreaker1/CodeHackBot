package orchestrator

import (
	"fmt"
	"strings"
)

const (
	RunPhasePlanning  = "planning"
	RunPhaseReview    = "review"
	RunPhaseApproved  = "approved"
	RunPhaseExecuting = "executing"
	RunPhaseCompleted = "completed"
)

var validRunPhases = map[string]struct{}{
	RunPhasePlanning:  {},
	RunPhaseReview:    {},
	RunPhaseApproved:  {},
	RunPhaseExecuting: {},
	RunPhaseCompleted: {},
}

func NormalizeRunPhase(raw string) string {
	phase := strings.ToLower(strings.TrimSpace(raw))
	if _, ok := validRunPhases[phase]; ok {
		return phase
	}
	return ""
}

func ValidateRunPhase(raw string) error {
	phase := NormalizeRunPhase(raw)
	if phase == "" {
		return fmt.Errorf("invalid run phase %q", raw)
	}
	return nil
}
