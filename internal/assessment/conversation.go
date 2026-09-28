package assessment

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

// PlanBrief gives both conversation surfaces the same bounded account of the
// coordinator's current decision. It describes a proposal, not a test result.
type PlanBrief struct {
	Phase           string   `json:"phase,omitempty"`
	Purpose         string   `json:"purpose"`
	PreviousResult  string   `json:"previous_result,omitempty"`
	Tasks           []string `json:"tasks,omitempty"`
	Gaps            []string `json:"gaps,omitempty"`
	ApprovedTaskIDs []string `json:"approved_task_ids,omitempty"`
	SkippedTaskIDs  []string `json:"skipped_task_ids,omitempty"`
}

func BriefPlan(plan Decision) PlanBrief {
	purpose := plan.PlainSummary
	if strings.TrimSpace(purpose) == "" {
		purpose = plan.Summary
	}
	brief := PlanBrief{
		Phase: plan.Phase, Purpose: promptExcerpt(purpose, 480),
		PreviousResult:  promptExcerpt(plan.Review, 480),
		ApprovedTaskIDs: append([]string(nil), plan.ApprovedTaskIDs...),
		SkippedTaskIDs:  append([]string(nil), plan.SkippedTaskIDs...),
	}
	for _, task := range plan.Tasks {
		brief.Tasks = append(brief.Tasks, task.ID+": "+promptExcerpt(task.Goal, 260)+"; intended result: "+promptExcerpt(task.DoneWhen, 220))
	}
	for _, gap := range plan.Gaps {
		if len(brief.Gaps) == 4 {
			break
		}
		brief.Gaps = append(brief.Gaps, promptExcerpt(gap, 260))
	}
	return brief
}

// ConversationRequest gives a live coordinator reply recent dialogue without
// making the transcript the source of truth for scope, evidence, or budgets.
// The current state and operator message are protected from history pruning.
func ConversationRequest(system, state string, prior []llmclient.Message, current llmclient.Message, maxBytes int) ([]llmclient.Message, error) {
	const omittedNote = "\nEarlier dialogue was omitted from this model request; the full transcript remains in the saved session."
	base := len(system) + len(state) + len(current.Content)
	if base > maxBytes {
		return nil, fmt.Errorf("coordinator conversation needs %d bytes for current state and message; limit is %d", base, maxBytes)
	}
	history := make([]llmclient.Message, 0, len(prior))
	historyBytes := 0
	for _, message := range prior {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		message.Attachments = nil // Saved attachments are references, not repeat model input.
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		history = append(history, message)
		historyBytes += len(message.Content)
	}
	if base+historyBytes > maxBytes {
		if maxBytes-base < len(omittedNote) {
			return nil, fmt.Errorf("coordinator conversation needs room to mark omitted history; current state uses %d of %d bytes", base, maxBytes)
		}
		// Keep an ordered tail only after the complete conversation no longer
		// fits. The saved transcript still owns the exact earlier messages.
		remaining := maxBytes - base - len(omittedNote)
		start := len(history)
		for start > 0 && remaining >= len(history[start-1].Content) {
			start--
			remaining -= len(history[start].Content)
		}
		history = history[start:]
		state += omittedNote
	}
	messages := make([]llmclient.Message, 0, len(history)+3)
	messages = append(messages, llmclient.Message{Role: "system", Content: system}, llmclient.Message{Role: "user", Content: state})
	messages = append(messages, history...)
	messages = append(messages, current)
	return messages, nil
}
