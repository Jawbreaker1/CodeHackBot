package workerloop

import (
	"fmt"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"strings"
)

func combineSummaries(stdout, stderr string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(stdout) != "" && stdout != "(none)" {
		parts = append(parts, "stdout: "+stdout)
	}
	if strings.TrimSpace(stderr) != "" && stderr != "(none)" {
		parts = append(parts, "stderr: "+stderr)
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " | ")
}

func compactOutputSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "(none)" {
		return "(none)"
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 2 {
		lines = lines[:2]
	}
	s = strings.Join(lines, "\n")
	const limit = 220
	if len(s) > limit {
		return strings.TrimSpace(s[:limit]) + "..."
	}
	return s
}

func preferredExecutionEvidence(result ctxpacket.ExecutionResult) string {
	if strings.TrimSpace(result.OutputEvidence) != "" && strings.TrimSpace(result.OutputEvidence) != "(none)" {
		return result.OutputEvidence
	}
	return result.OutputSummary
}

func buildRunningSummary(objective string, latest ctxpacket.ExecutionResult, recent []ctxpacket.ExecutionResult) string {
	truth := latest
	const (
		runningSummaryActionMax   = 320
		runningSummaryEvidenceMax = 1200
	)
	status := "in progress"
	if ctxpacket.IsInterruptedResult(truth) {
		status = "in progress"
	} else if strings.TrimSpace(truth.Assessment) == "failed" || strings.TrimSpace(truth.FailureClass) != "" || strings.TrimSpace(truth.ExitStatus) == "-1" {
		status = "encountered a failure"
	}
	if strings.TrimSpace(truth.ExitStatus) != "" && strings.TrimSpace(truth.ExitStatus) != "(none)" && strings.TrimSpace(truth.ExitStatus) != "0" {
		if !ctxpacket.IsInterruptedResult(truth) {
			status = "encountered a failure"
		}
	}
	if strings.TrimSpace(truth.Assessment) == "suspicious" || strings.TrimSpace(truth.Assessment) == "ambiguous" {
		status = "needs interpretation"
	}
	if ctxpacket.IsInterruptedResult(truth) {
		status = "in progress"
	}

	parts := []string{fmt.Sprintf("Status: %s.", status)}
	if strings.TrimSpace(truth.Action) != "" {
		parts = append(parts, fmt.Sprintf("Evidence: %q exited with %s.", compactInline(truth.Action, runningSummaryActionMax), blankOrFallback(strings.TrimSpace(truth.ExitStatus), "(none)")))
	}
	if ctxpacket.IsInterruptedResult(truth) {
		parts = append(parts, "Execution was interrupted before the active work completed.")
	}
	if strings.TrimSpace(truth.Assessment) != "" && strings.TrimSpace(truth.Assessment) != "(none)" {
		parts = append(parts, fmt.Sprintf("Assessment: %s.", truth.Assessment))
	}
	if len(truth.Signals) > 0 {
		parts = append(parts, fmt.Sprintf("Signals: %s.", strings.Join(truth.Signals, ", ")))
	}
	if evidence := preferredExecutionEvidence(truth); strings.TrimSpace(evidence) != "" && strings.TrimSpace(evidence) != "(none)" {
		parts = append(parts, fmt.Sprintf("Key output: %s.", compactInline(singleLine(evidence), runningSummaryEvidenceMax)))
	}
	return strings.Join(parts, " ")
}

func singleLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " | ")
	return s
}

func blankOrFallback(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func compactInline(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return strings.TrimSpace(s[:max-3]) + "..."
}

func blank(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
