package orchestrator

import "strings"

func latestAssistResultSummary(feedback *assistExecutionFeedback) string {
	if feedback == nil {
		return ""
	}
	return strings.TrimSpace(feedback.ResultSummary)
}

func latestAssistEvidenceRefs(feedback *assistExecutionFeedback) []string {
	if feedback == nil {
		return nil
	}
	return append([]string{}, feedback.PrimaryArtifactRefs...)
}

func latestAssistInputRefs(feedback *assistExecutionFeedback) []string {
	if feedback == nil {
		return nil
	}
	return append([]string{}, feedback.InputPathRefs...)
}

func latestAssistLogPath(feedback *assistExecutionFeedback) string {
	if feedback == nil {
		return ""
	}
	return strings.TrimSpace(feedback.LogPath)
}

func latestAssistFailureDetails(feedback *assistExecutionFeedback) map[string]any {
	if feedback == nil {
		return nil
	}
	details := map[string]any{}
	if summary := latestAssistResultSummary(feedback); summary != "" {
		details["latest_result_summary"] = summary
	}
	if refs := latestAssistEvidenceRefs(feedback); len(refs) > 0 {
		details["latest_evidence_refs"] = refs
	}
	if refs := latestAssistInputRefs(feedback); len(refs) > 0 {
		details["latest_input_refs"] = refs
	}
	if path := latestAssistLogPath(feedback); path != "" {
		details["latest_log_path"] = path
	}
	return details
}

func mergeFailureDetails(base map[string]any, overlay map[string]any) map[string]any {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}
