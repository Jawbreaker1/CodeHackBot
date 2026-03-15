package cli

import (
	"fmt"
	"strings"
)

func shouldRequirePlanConfirmation(steps []string) bool {
	return len(steps) > 0
}

func complexPlanConfirmationReasons(goal string, steps []string, planText string) []string {
	reasons := make([]string, 0, 4)
	addReason := func(reason string) {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return
		}
		for _, existing := range reasons {
			if strings.EqualFold(existing, reason) {
				return
			}
		}
		reasons = append(reasons, reason)
	}

	if len(steps) >= 4 {
		addReason(fmt.Sprintf("%d planned steps", len(steps)))
	}

	goalLower := strings.ToLower(strings.TrimSpace(goal))
	goalHasCIDR := cidrPattern.MatchString(goalLower)
	for _, step := range steps {
		lower := strings.ToLower(strings.TrimSpace(step))
		if lower == "" {
			continue
		}
		if cidrPattern.MatchString(lower) && !goalHasCIDR {
			addReason("plan expands to subnet/network-wide scope")
		}
		if strings.Contains(lower, "find /") || strings.Contains(lower, " -r /") {
			addReason("plan includes broad filesystem search")
		}
		if strings.Contains(lower, "brute force") || strings.Contains(lower, "bruteforce") || strings.Contains(lower, "wordlist") || strings.Contains(lower, "dictionary") {
			addReason("plan includes password-guessing workflow")
		}
		if strings.Contains(lower, "-p-") || strings.Contains(lower, "all ports") || strings.Contains(lower, "--script vuln") {
			addReason("plan includes high-cost scan depth")
		}
	}

	if len(strings.TrimSpace(planText)) > 900 {
		addReason("large plan narrative")
	}

	return reasons
}

func (r *Runner) maybeConfirmComplexPlanExecution(goal string, steps []string, planText string) (bool, error) {
	if !shouldRequirePlanConfirmation(steps) {
		return true, nil
	}
	if !r.interactiveSession || !r.isTTY() {
		return true, nil
	}
	trimmedGoal := strings.TrimSpace(goal)
	if trimmedGoal == "" {
		trimmedGoal = strings.TrimSpace(r.assistRuntime.Goal)
	}
	if trimmedGoal != "" && r.assistExecApproved && strings.EqualFold(strings.TrimSpace(r.assistExecGoal), trimmedGoal) {
		return true, nil
	}
	reasons := complexPlanConfirmationReasons(goal, steps, planText)
	safePrintln("Plan ready. Review before execution:")
	if trimmedGoal != "" {
		safePrintf("- Goal: %s\n", truncate(trimmedGoal, 220))
	}
	safePrintf("- Steps: %d\n", len(steps))
	for _, reason := range reasons {
		safePrintf("- Reason: %s\n", reason)
	}
	if len(steps) > 0 {
		safePrintln("Planned steps listed above.")
	}
	approved, err := r.confirm("Execute this plan now?")
	if err != nil {
		return false, err
	}
	if !approved {
		msg := "Plan execution canceled by user. Provide revisions or re-run /assist when ready."
		safePrintln(msg)
		r.appendConversation("Assistant", msg)
		if trimmedGoal != "" {
			r.pendingAssistGoal = trimmedGoal
		}
		r.pendingAssistQ = "Plan execution requires explicit approval."
		return false, nil
	}
	if trimmedGoal != "" {
		r.assistExecApproved = true
		r.assistExecGoal = trimmedGoal
	}
	return true, nil
}
