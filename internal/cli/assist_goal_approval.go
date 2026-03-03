package cli

import "strings"

func (r *Runner) maybeConfirmGoalExecution(goal string, dryRun bool) (bool, error) {
	if dryRun || !r.interactiveSession || !r.isTTY() || !r.isActionGoal(goal) {
		return true, nil
	}
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return true, nil
	}
	if r.assistExecApproved && strings.EqualFold(strings.TrimSpace(r.assistExecGoal), goal) {
		return true, nil
	}

	previewSummary := ""
	previewAction := ""
	previewSteps := []string{}

	if r.llmAllowed() {
		stopIndicator := r.startLLMIndicatorIfAllowed("plan preview")
		preview, err := r.getAssistSuggestion(goal, "next-steps")
		stopIndicator()
		if err == nil {
			switch strings.ToLower(strings.TrimSpace(preview.Type)) {
			case "plan":
				previewSteps = append(previewSteps, preview.Steps...)
				previewSummary = collapseWhitespace(strings.TrimSpace(firstNonEmpty(preview.Plan, preview.Summary)))
			case "command", "tool":
				previewSummary = collapseWhitespace(strings.TrimSpace(preview.Summary))
				if previewSummary == "" {
					previewSummary = assistStepDescription(preview)
				}
				previewAction = assistActionPreview(preview)
			case "question":
				previewSummary = collapseWhitespace(strings.TrimSpace(preview.Question))
			}
		}
	}

	safePrintln("Execution approval required:")
	safePrintf("- Goal: %s\n", truncate(goal, 220))
	if previewSummary != "" {
		safePrintf("- Proposed approach: %s\n", truncate(previewSummary, 220))
	}
	if len(previewSteps) > 0 {
		safePrintln("Proposed steps:")
		limit := len(previewSteps)
		if limit > 8 {
			limit = 8
		}
		for i := 0; i < limit; i++ {
			safePrintf("%d) %s\n", i+1, previewSteps[i])
		}
		if len(previewSteps) > limit {
			safePrintf("... (%d more step(s))\n", len(previewSteps)-limit)
		}
	}
	if previewAction != "" {
		safePrintf("- First action candidate: %s\n", truncate(previewAction, 220))
	}
	approved, err := r.confirm("Approve execution for this goal?")
	if err != nil {
		return false, err
	}
	if !approved {
		msg := "Execution paused by user approval gate. Reply with `yes` or a revised instruction to continue."
		safePrintln(msg)
		r.appendConversation("Assistant", msg)
		r.pendingAssistGoal = goal
		r.pendingAssistQ = "Execution approval required."
		return false, nil
	}
	r.assistExecApproved = true
	r.assistExecGoal = goal
	return true, nil
}
