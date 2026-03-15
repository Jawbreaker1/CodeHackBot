package session

import (
	"fmt"
	"strings"
)

// Foundation is the minimal session/run foundation used by the rebuild path.
type Foundation struct {
	Goal                 string
	ReportingRequirement string
}

// Input is the external construction form for a foundation.
type Input struct {
	Goal                 string
	ReportingRequirement string
}

// NewFoundation constructs the minimal session foundation.
func NewFoundation(in Input) (Foundation, error) {
	goal := strings.TrimSpace(in.Goal)
	if goal == "" {
		return Foundation{}, fmt.Errorf("goal is required")
	}

	reporting := strings.TrimSpace(in.ReportingRequirement)
	if reporting == "" {
		reporting = "owasp"
	}

	return Foundation{
		Goal:                 goal,
		ReportingRequirement: reporting,
	}, nil
}
