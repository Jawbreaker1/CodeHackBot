package cli

import "strings"

func (r *Runner) assistLoopMode() string {
	mode := strings.ToLower(strings.TrimSpace(r.cfg.Agent.AssistLoopMode))
	switch mode {
	case "open_like":
		return "open_like"
	default:
		return "strict"
	}
}

func (r *Runner) openLikeAssistLoopEnabled() bool {
	return r.assistLoopMode() == "open_like"
}

func (r *Runner) assistRepairAttempts() int {
	if r.cfg.Agent.AssistRepairAttempts > 0 {
		return r.cfg.Agent.AssistRepairAttempts
	}
	if r.openLikeAssistLoopEnabled() {
		return 3
	}
	return 1
}

func (r *Runner) isActionGoal(goal string) bool {
	return looksLikeAction(strings.TrimSpace(goal))
}
