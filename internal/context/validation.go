package context

import (
	"fmt"
	"strings"
)

type ValidationSeverity string

const (
	ValidationInfo  ValidationSeverity = "info"
	ValidationWarn  ValidationSeverity = "warn"
	ValidationError ValidationSeverity = "error"
	ValidationFatal ValidationSeverity = "fatal"
)

type ValidationIssue struct {
	Severity ValidationSeverity
	Code     string
	Message  string
}

type ValidationReport struct {
	Issues []ValidationIssue
}

func ValidatePacket(packet WorkerPacket) ValidationReport {
	report := ValidationReport{}

	sections := packet.RenderSections()
	seen := make(map[string]struct{}, len(sections))
	for _, section := range sections {
		if _, ok := seen[section.Name]; ok {
			report.add(ValidationFatal, "duplicate_section", fmt.Sprintf("duplicate rendered section: %s", section.Name))
			continue
		}
		seen[section.Name] = struct{}{}
	}

	if strings.TrimSpace(packet.BehaviorFrame.PromptText()) == "" {
		report.add(ValidationFatal, "missing_behavior_frame", "behavior_frame is empty")
	}
	if strings.TrimSpace(packet.SessionFoundation.Goal) == "" {
		report.add(ValidationFatal, "missing_goal", "session goal is empty")
	}
	if strings.TrimSpace(packet.CurrentStep.Objective) == "" {
		report.add(ValidationError, "missing_objective", "current_step.objective is empty")
	}
	if strings.TrimSpace(packet.RunningSummary) == "" {
		report.add(ValidationWarn, "missing_running_summary", "running_summary is empty")
	}
	if packet.TaskRuntime.State == "done" && strings.TrimSpace(packet.TaskRuntime.MissingFact) != "(none)" {
		report.add(ValidationError, "done_with_missing_fact", "task_runtime.state is done but missing_fact is not '(none)'")
	}
	if target := strings.TrimSpace(packet.TaskRuntime.CurrentTarget); target != "" && isSessionRuntimeArtifactPath(target) {
		report.add(ValidationError, "runtime_artifact_as_target", "task_runtime.current_target points at a session runtime artifact")
	}
	if len(packet.RecentConversation) > recentConversationTurnLimit {
		report.add(ValidationError, "recent_conversation_turn_overflow", fmt.Sprintf("recent_conversation has %d turns; limit is %d", len(packet.RecentConversation), recentConversationTurnLimit))
	}
	if approxConversationTokens(packet.RecentConversation) > recentConversationTokenLimit {
		report.add(ValidationError, "recent_conversation_token_overflow", fmt.Sprintf("recent_conversation exceeds token limit %d", recentConversationTokenLimit))
	}
	if hasAnyPendingField(packet.OperatorState) {
		if strings.TrimSpace(packet.OperatorState.PendingAction) == "" {
			report.add(ValidationError, "pending_without_action", "operator_state has pending execution fields without pending_action")
		}
		if strings.TrimSpace(packet.OperatorState.PendingAction) != "" && strings.TrimSpace(packet.OperatorState.PendingExec) == "" {
			report.add(ValidationError, "pending_without_exec", "operator_state.pending_action is set but pending_exec is empty")
		}
		if strings.TrimSpace(packet.OperatorState.PendingAction) != "" && strings.TrimSpace(packet.OperatorState.PendingMode) == "" {
			report.add(ValidationWarn, "pending_without_mode", "operator_state.pending_action is set but pending_mode is empty")
		}
	}
	if hasPartialLatestExecution(packet.LatestExecutionResult) {
		report.add(ValidationError, "partial_latest_execution_result", "latest_execution_result is partially populated")
	}

	return report
}

func (r ValidationReport) HighestSeverity() ValidationSeverity {
	highest := ValidationInfo
	for _, issue := range r.Issues {
		if severityRank(issue.Severity) > severityRank(highest) {
			highest = issue.Severity
		}
	}
	return highest
}

func (r ValidationReport) IsFatal() bool {
	return r.HighestSeverity() == ValidationFatal
}

func (r ValidationReport) Summary() string {
	if len(r.Issues) == 0 {
		return "ok"
	}
	return fmt.Sprintf("%s (%d issue(s))", r.HighestSeverity(), len(r.Issues))
}

func (r *ValidationReport) add(severity ValidationSeverity, code, message string) {
	r.Issues = append(r.Issues, ValidationIssue{
		Severity: severity,
		Code:     code,
		Message:  message,
	})
}

func severityRank(severity ValidationSeverity) int {
	switch severity {
	case ValidationFatal:
		return 4
	case ValidationError:
		return 3
	case ValidationWarn:
		return 2
	case ValidationInfo:
		return 1
	default:
		return 0
	}
}

func hasAnyPendingField(state OperatorState) bool {
	return strings.TrimSpace(state.PendingAction) != "" ||
		strings.TrimSpace(state.PendingMode) != "" ||
		strings.TrimSpace(state.PendingExec) != "" ||
		strings.TrimSpace(state.PendingLog) != ""
}

func hasPartialLatestExecution(result ExecutionResult) bool {
	action := strings.TrimSpace(result.Action)
	exit := strings.TrimSpace(result.ExitStatus)
	assessment := strings.TrimSpace(result.Assessment)
	output := strings.TrimSpace(result.OutputSummary)
	hasRefs := len(result.LogRefs) > 0 || len(result.ArtifactRefs) > 0

	if action == "" {
		return exit != "" || assessment != "" || output != "" || hasRefs || strings.TrimSpace(result.FailureClass) != "" || len(result.Signals) > 0
	}
	return exit == ""
}
