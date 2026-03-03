package cli

import (
	"fmt"
	"regexp"
	"strings"
)

var numberedChoicePattern = regexp.MustCompile(`\(\d+\)`)

func formatAssistQuestionForUser(question string, summary string) string {
	question = collapseWhitespace(strings.TrimSpace(question))
	summary = collapseWhitespace(strings.TrimSpace(summary))
	if question == "" {
		return "Input required."
	}

	choices, header := extractNumberedChoices(question)
	builder := strings.Builder{}
	builder.WriteString("Input required:")
	if header != "" {
		builder.WriteString("\n" + header)
	} else {
		builder.WriteString("\n" + question)
	}
	if len(choices) > 0 {
		builder.WriteString("\nOptions:")
		for i, choice := range choices {
			builder.WriteString(fmt.Sprintf("\n%d. %s", i+1, choice))
		}
		builder.WriteString("\nReply with the option number or a short instruction.")
	}
	if summary != "" && !strings.Contains(strings.ToLower(summary), strings.ToLower(question)) {
		builder.WriteString("\nWhy this is asked: " + summary)
	}
	return builder.String()
}

func extractNumberedChoices(question string) ([]string, string) {
	indexes := numberedChoicePattern.FindAllStringIndex(question, -1)
	if len(indexes) < 2 {
		return nil, ""
	}
	firstStart := indexes[0][0]
	header := strings.TrimSpace(question[:firstStart])
	header = strings.TrimRight(header, " :,;")

	choices := make([]string, 0, len(indexes))
	for i, idx := range indexes {
		start := idx[1]
		end := len(question)
		if i+1 < len(indexes) {
			end = indexes[i+1][0]
		}
		segment := strings.TrimSpace(question[start:end])
		segment = strings.Trim(segment, " ,;.")
		segment = strings.TrimPrefix(segment, "or ")
		segment = strings.TrimPrefix(segment, "and ")
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		choices = append(choices, segment)
	}
	if len(choices) < 2 {
		return nil, ""
	}
	return choices, header
}
