package guided

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

// The coordinator owns the proposal. The terminal adapter only collects the
// operator's selection, using the same approval-mode rule as the web adapter.
func reviewCoordinatorPlan(ctx context.Context, c *Console, plan assessment.Decision) (assessment.PlanReview, error) {
	all := make([]string, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		all = append(all, task.ID)
	}
	var presentation strings.Builder
	fmt.Fprintf(&presentation, "\nCoordinator plan: %s\n", plan.Summary)
	if plan.Review != "" {
		fmt.Fprintf(&presentation, "\nPrevious round: %s\n", plan.Review)
	}
	if plan.PlainSummary != "" {
		fmt.Fprintf(&presentation, "\nPurpose: %s\n", plan.PlainSummary)
	}
	presentation.WriteString("\nTasks\n")
	for i, task := range plan.Tasks {
		fmt.Fprintf(&presentation, "  %d. %s — %s\n     Done when: %s\n", i+1, task.ID, task.Goal, task.DoneWhen)
	}
	if len(plan.OpenLeads) > 0 {
		presentation.WriteString("\nOpen questions (unverified):\n")
		for _, lead := range plan.OpenLeads {
			fmt.Fprintf(&presentation, "  - %s\n    Next check: %s\n", lead.Hypothesis, lead.NextCheck)
		}
	}
	if len(plan.ClosedLeads) > 0 {
		presentation.WriteString("\nQuestions resolved in this round:\n")
		for _, lead := range plan.ClosedLeads {
			fmt.Fprintf(&presentation, "  - %s: %s\n", lead.ID, lead.Reason)
		}
	}
	c.Print("%s", presentation.String())
	if c.approvalMode().Normalized() != approval.EveryExecution {
		c.Print("Starting these tasks under the selected approval level.\n")
		return assessment.PlanReview{TaskIDs: all}, nil
	}
	for {
		answer, err := c.Ask(ctx, "Run all proposed tasks? [Enter=all, numbers=select, r=revise]")
		if err != nil {
			return assessment.PlanReview{}, err
		}
		switch strings.ToLower(answer) {
		case "", "all", "y", "yes":
			return assessment.PlanReview{TaskIDs: all}, nil
		case "r", "revise", "n", "no":
			direction, err := required(ctx, c, "What should the coordinator change about this plan?")
			return assessment.PlanReview{Revision: direction}, err
		}
		selected := make([]string, 0, len(plan.Tasks))
		seen := make(map[int]bool)
		valid := true
		for _, field := range strings.Fields(strings.ReplaceAll(answer, ",", " ")) {
			index, err := strconv.Atoi(field)
			if err != nil || index < 1 || index > len(plan.Tasks) {
				valid = false
				break
			}
			if !seen[index] {
				selected = append(selected, all[index-1])
				seen[index] = true
			}
		}
		if valid && len(selected) > 0 {
			return assessment.PlanReview{TaskIDs: selected}, nil
		}
		c.Print("Choose task numbers from 1 to %d, press Enter for all, or ask for a revision.\n", len(plan.Tasks))
	}
}
