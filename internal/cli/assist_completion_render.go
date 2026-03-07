package cli

import (
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func renderCompletionMessage(suggestion assist.Suggestion, final string) string {
	final = strings.TrimSpace(final)
	if looksLikeInternalCompletionLeak(final) {
		final = ""
	}
	if final != "" {
		return final
	}
	why := collapseWhitespace(strings.TrimSpace(normalizeAssistantOutput(suggestion.WhyMet)))
	if suggestion.ObjectiveMet != nil && !*suggestion.ObjectiveMet {
		if why != "" {
			return "Objective not met. " + truncate(why, 240)
		}
		return "Objective not met."
	}
	if why != "" {
		return truncate(why, 240)
	}
	return "(completed)"
}

func looksLikeInternalCompletionLeak(text string) bool {
	lower := strings.ToLower(collapseWhitespace(strings.TrimSpace(text)))
	if lower == "" {
		return false
	}
	markers := []string{
		"analyze the request",
		"analyze the input data",
		"evaluate current state",
		"evaluate success/failure",
		"synthesize findings",
		"self-correction",
		"task foundation:",
		"final review against constraints",
		"constructing output",
		"summary:** mentions recent actions",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

