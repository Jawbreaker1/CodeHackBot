package context

import "strings"

func IsInterruptedResult(result ExecutionResult) bool {
	if strings.TrimSpace(result.FailureClass) == "execution_interrupted" {
		return true
	}
	for _, signal := range result.Signals {
		switch strings.TrimSpace(signal) {
		case "execution_interrupted", "execution_timeout":
			return true
		}
	}
	return false
}
