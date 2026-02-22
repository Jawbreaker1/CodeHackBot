package main

import "strings"

func tuiBarStyleForRunState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "completed":
		return tuiStyleBarGood
	case "running", "executing":
		return tuiStyleBar
	case "planning", "review", "approved":
		return tuiStyleBarWarn
	case "stopped", "failed", "blocked":
		return tuiStyleBarBad
	default:
		return tuiStyleBar
	}
}

func colorizeTUIRows(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		trimmed := strings.TrimSpace(row)
		if trimmed == "" || strings.Contains(row, "\x1b[") {
			out = append(out, row)
			continue
		}
		style := tuiLineStyle(row)
		if style == "" {
			out = append(out, row)
			continue
		}
		out = append(out, style+row+tuiStyleReset)
	}
	return out
}

func tuiLineStyle(line string) string {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return ""
	}
	if strings.HasPrefix(lower, "updated:") {
		return tuiStyleMuted
	}
	if strings.Contains(lower, "completed:") && strings.Contains(lower, "failed:") {
		if strings.Contains(lower, "failed: 0") {
			return tuiStyleGood
		}
		return tuiStyleWarn
	}
	if hasAny(lower,
		"task_failed",
		"reason=failed",
		"reason=execution_timeout",
		"reason=budget_exhausted",
		"assist_loop_detected",
		"scope_denied",
		"policy_denied",
		"command_timeout",
		"error:",
		"state=failed",
		"[failed]",
		"[!]",
	) {
		return tuiStyleBad
	}
	if hasAny(lower,
		"awaiting_approval",
		"pending approvals",
		"approval_requested",
		"approval_",
		"state=blocked",
		"state=awaiting_approval",
		"[blocked]",
		"[-]",
	) {
		return tuiStyleWarn
	}
	if hasAny(lower,
		"state=running",
		"state=active",
		"[running]",
		"[>]",
		"running:",
		"current:",
		"worker active",
	) {
		return tuiStyleGood
	}
	if hasAny(lower,
		"[completed]",
		"[x]",
		"report (ready)",
		"task_completed",
		"state=completed",
		"run completed",
	) {
		return tuiStyleInfo
	}
	if hasAny(lower,
		"state=stopped",
		"state=canceled",
		"[~]",
		"collapsed workers",
		"no workers running",
	) {
		return tuiStyleMuted
	}
	return ""
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
