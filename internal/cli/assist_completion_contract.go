package cli

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func (r *Runner) validateAssistCompletionContract(suggestion assist.Suggestion) error {
	goal := strings.TrimSpace(r.assistRuntime.Goal)
	if goal == "" || !looksLikeAction(goal) {
		return nil
	}
	finalText := strings.TrimSpace(firstNonEmpty(suggestion.Final, suggestion.Summary))
	if looksLikeConversationalCompletion(finalText) {
		return fmt.Errorf("assistant completion contract: conversational reply is not a valid completion for action goals")
	}
	if suggestion.ObjectiveMet == nil {
		return fmt.Errorf("assistant completion contract: objective_met is required for action goals")
	}
	objectiveMet := *suggestion.ObjectiveMet
	if err := validateCompletionNarrativeConsistency(finalText, objectiveMet); err != nil {
		return err
	}
	if strings.TrimSpace(suggestion.WhyMet) == "" {
		return fmt.Errorf("assistant completion contract: why_met is required for action goals")
	}
	if len(suggestion.EvidenceRefs) == 0 {
		return fmt.Errorf("assistant completion contract: evidence_refs are required for action completion claims")
	}
	for _, ref := range suggestion.EvidenceRefs {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("assistant completion contract: evidence_refs contain empty value")
		}
	}
	resolved, missing := r.resolveCompletionEvidenceRefs(suggestion.EvidenceRefs)
	enforceReadable := r.shouldEnforceResolvedEvidenceRefs(suggestion.EvidenceRefs)
	if enforceReadable {
		if len(resolved) == 0 {
			return fmt.Errorf("assistant completion contract: no readable evidence refs found")
		}
		if len(missing) > 0 {
			return fmt.Errorf("assistant completion contract: unresolved evidence refs: %s", strings.Join(missing, ", "))
		}
	}
	if !objectiveMet {
		if len(resolved) == 0 {
			return fmt.Errorf("assistant completion contract: objective_not_met requires at least one readable evidence ref")
		}
		return nil
	}
	if len(resolved) > 0 {
		if err := r.verifyCompletionEvidenceSemantics(goal, suggestion, resolved); err != nil {
			return err
		}
	}
	if len(resolved) == 0 && r.cfg.UI.Verbose {
		r.logger.Printf("Completion contract: skipping semantic check (no readable evidence refs)")
	}
	return nil
}

func looksLikeConversationalCompletion(text string) bool {
	text = strings.ToLower(collapseWhitespace(strings.TrimSpace(text)))
	if text == "" {
		return false
	}
	if strings.HasPrefix(text, "hello") || strings.HasPrefix(text, "hi ") || strings.HasPrefix(text, "hey") || strings.HasPrefix(text, "welcome") {
		return true
	}
	if strings.Contains(text, "how can i help") {
		return true
	}
	if strings.Contains(text, "what is the scope of this engagement") {
		return true
	}
	if strings.Contains(text, "specific targets to focus on") {
		return true
	}
	if strings.Contains(text, "type of assessment are you looking for") {
		return true
	}
	return false
}

func validateCompletionNarrativeConsistency(finalText string, objectiveMet bool) error {
	lower := strings.ToLower(collapseWhitespace(strings.TrimSpace(finalText)))
	if lower == "" {
		return nil
	}
	if objectiveMet {
		deny := []string{
			"unverified",
			"cannot prove",
			"can't prove",
			"could not prove",
			"objective not met",
			"goal not met",
			"not completed",
			"incomplete",
			"failed",
		}
		for _, token := range deny {
			if strings.Contains(lower, token) {
				return fmt.Errorf("assistant completion contract: objective_met=true contradicts final narrative (%q)", token)
			}
		}
		return nil
	}
	deny := []string{
		"objective status: met",
		"objective met",
		"goal completed",
		"completed successfully",
		"successfully completed",
	}
	for _, token := range deny {
		if strings.Contains(lower, token) {
			return fmt.Errorf("assistant completion contract: objective_met=false contradicts final narrative (%q)", token)
		}
	}
	return nil
}
