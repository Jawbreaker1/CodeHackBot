package cli

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func (r *Runner) validateAssistDecisionContract(suggestion assist.Suggestion) error {
	goal := strings.TrimSpace(r.assistRuntime.Goal)
	if goal == "" || !looksLikeAction(goal) {
		return nil
	}
	openLike := r.openLikeAssistLoopEnabled()
	decision := strings.ToLower(strings.TrimSpace(suggestion.Decision))
	switch suggestion.Type {
	case "command", "tool":
		if openLike {
			return nil
		}
		if decision == "" {
			return fmt.Errorf("assistant decision contract: decision is required for %s", suggestion.Type)
		}
		if decision != "retry_modified" && decision != "pivot_strategy" {
			return fmt.Errorf("assistant decision contract: invalid decision %q for %s", decision, suggestion.Type)
		}
	case "question":
		if openLike {
			return nil
		}
		if decision != "ask_user" {
			return fmt.Errorf("assistant decision contract: question requires decision=ask_user")
		}
	case "complete":
		if decision != "step_complete" {
			return fmt.Errorf("assistant decision contract: complete requires decision=step_complete")
		}
	}
	return nil
}

func (r *Runner) normalizeAssistDecision(suggestion assist.Suggestion) (assist.Suggestion, string) {
	goal := strings.TrimSpace(r.assistRuntime.Goal)
	if goal == "" || !looksLikeAction(goal) {
		return suggestion, ""
	}
	decision := strings.ToLower(strings.TrimSpace(suggestion.Decision))
	switch suggestion.Type {
	case "command", "tool":
		if decision == "retry_modified" || decision == "pivot_strategy" {
			return suggestion, ""
		}
		prev := strings.TrimSpace(suggestion.Decision)
		suggestion.Decision = "retry_modified"
		if prev == "" {
			prev = "(empty)"
		}
		return suggestion, fmt.Sprintf("Decision normalized for %s: %s -> retry_modified", suggestion.Type, prev)
	case "question":
		if decision == "ask_user" {
			return suggestion, ""
		}
		prev := strings.TrimSpace(suggestion.Decision)
		suggestion.Decision = "ask_user"
		if prev == "" {
			prev = "(empty)"
		}
		return suggestion, fmt.Sprintf("Decision normalized for question: %s -> ask_user", prev)
	default:
		return suggestion, ""
	}
}
