package context

import "strings"

type ResultImpact string

const (
	ResultImpactInformational ResultImpact = "informational"
	ResultImpactRecoverable   ResultImpact = "recoverable"
	ResultImpactBlocking      ResultImpact = "blocking"
)

// ActiveTruthResult returns the strongest active evidence result to use when
// deriving runtime state and summaries. The latest execution result remains the
// raw latest truth; this selector is only for choosing the most informative
// active evidence when recent retained results are stronger.
func ActiveTruthResult(latest ExecutionResult, recent []ExecutionResult) ExecutionResult {
	best := latest
	bestRank := truthRank(latest)
	for _, candidate := range recent {
		rank := truthRank(candidate)
		if rank > bestRank {
			best = candidate
			bestRank = rank
		}
	}
	return best
}

func truthRank(result ExecutionResult) int {
	if strings.TrimSpace(result.Action) == "" {
		return 0
	}
	if IsInterruptedResult(result) {
		return 250 + signalRank(result.Signals)
	}
	if strings.TrimSpace(result.Assessment) == "failed" || strings.TrimSpace(result.FailureClass) != "" || nonzeroExit(result.ExitStatus) {
		return 400 + signalRank(result.Signals)
	}
	if strings.TrimSpace(result.Assessment) == "suspicious" || len(result.Signals) > 0 {
		return 300 + signalRank(result.Signals)
	}
	if strings.TrimSpace(result.Assessment) == "ambiguous" {
		return 200 + signalRank(result.Signals)
	}
	return 100 + signalRank(result.Signals)
}

func nonzeroExit(exitStatus string) bool {
	exitStatus = strings.TrimSpace(exitStatus)
	return exitStatus != "" && exitStatus != "(none)" && exitStatus != "0"
}

func signalRank(signals []string) int {
	best := 0
	for _, signal := range signals {
		score := 1
		switch strings.TrimSpace(signal) {
		case "incorrect_password", "permission_denied":
			score = 30
		case "execution_timeout":
			score = 20
		case "execution_interrupted":
			score = 15
		case "missing_path":
			score = 10
		}
		if score > best {
			best = score
		}
	}
	return best
}

func ImpactOfResult(result ExecutionResult) ResultImpact {
	if strings.TrimSpace(result.Action) == "" {
		return ResultImpactInformational
	}
	if IsInterruptedResult(result) {
		return ResultImpactRecoverable
	}
	for _, signal := range result.Signals {
		switch strings.TrimSpace(signal) {
		case "missing_path", "permission_denied", "invalid_action", "command_not_found":
			return ResultImpactBlocking
		}
	}
	if strings.TrimSpace(result.Assessment) == "failed" || strings.TrimSpace(result.FailureClass) != "" || nonzeroExit(result.ExitStatus) {
		return ResultImpactRecoverable
	}
	return ResultImpactInformational
}

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
