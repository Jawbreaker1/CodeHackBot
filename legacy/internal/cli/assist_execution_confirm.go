package cli

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func (r *Runner) maybeConfirmAssistExecution(suggestion assist.Suggestion, dryRun bool) (bool, error) {
	if dryRun || !r.interactiveSession || !r.isTTY() || !r.assistRuntime.Active {
		return true, nil
	}
	goal := strings.TrimSpace(r.assistRuntime.Goal)
	if goal == "" {
		return true, nil
	}
	if r.assistExecApproved && strings.EqualFold(strings.TrimSpace(r.assistExecGoal), goal) {
		return true, nil
	}

	summary := collapseWhitespace(strings.TrimSpace(suggestion.Summary))
	if summary == "" {
		summary = assistStepDescription(suggestion)
	}
	action := assistActionPreview(suggestion)
	safePrintln("Execution preview:")
	safePrintf("- Goal: %s\n", truncate(goal, 220))
	if summary != "" {
		safePrintf("- Plan/Summary: %s\n", truncate(summary, 220))
	}
	if action != "" {
		safePrintf("- Next action: %s\n", truncate(action, 220))
	}

	ok, err := r.confirm("Approve execution now?")
	if err != nil {
		return false, err
	}
	if !ok {
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

func assistActionPreview(suggestion assist.Suggestion) string {
	switch strings.ToLower(strings.TrimSpace(suggestion.Type)) {
	case "command":
		return strings.TrimSpace(strings.Join(append([]string{suggestion.Command}, suggestion.Args...), " "))
	case "tool":
		if suggestion.Tool == nil {
			return "tool action"
		}
		cmd := strings.TrimSpace(strings.Join(append([]string{suggestion.Tool.Run.Command}, suggestion.Tool.Run.Args...), " "))
		if cmd == "" {
			cmd = strings.TrimSpace(suggestion.Tool.Name)
		}
		if cmd == "" {
			return "tool action"
		}
		return fmt.Sprintf("tool: %s", cmd)
	default:
		return ""
	}
}
