package assessment

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

// ConversationRequest gives a live coordinator reply recent dialogue without
// making the transcript the source of truth for scope, evidence, or budgets.
// The current state and operator message are protected from history pruning.
func ConversationRequest(system, state string, prior []llmclient.Message, current llmclient.Message, maxBytes int) ([]llmclient.Message, error) {
	const maxHistoryMessages = 16
	const maxHistoryBytes = 24 << 10
	const maxMessageBytes = 4096
	const omittedNote = "\nEarlier dialogue was omitted from this model request; the full transcript remains in the saved session."
	base := len(system) + len(state) + len(current.Content)
	if base > maxBytes {
		return nil, fmt.Errorf("coordinator conversation needs %d bytes for current state and message; limit is %d", base, maxBytes)
	}
	remaining := min(maxBytes-base-len(omittedNote), maxHistoryBytes)
	if remaining < 0 {
		remaining = 0
	}
	var recent []llmclient.Message
	for i := len(prior) - 1; i >= 0 && len(recent) < maxHistoryMessages; i-- {
		message := prior[i]
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		message.Content = strings.TrimSpace(message.Content)
		message.Attachments = nil // Saved attachments are references, not repeat model input.
		if message.Content == "" {
			continue
		}
		message.Content = promptExcerptEnds(message.Content, maxMessageBytes)
		if len(message.Content) > remaining {
			break // Keep an ordered, contiguous tail of the discussion.
		}
		recent = append(recent, message)
		remaining -= len(message.Content)
	}
	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}
	if len(recent) < len(prior) && base+len(omittedNote) <= maxBytes {
		state += omittedNote
	}
	messages := make([]llmclient.Message, 0, len(recent)+3)
	messages = append(messages, llmclient.Message{Role: "system", Content: system}, llmclient.Message{Role: "user", Content: state})
	messages = append(messages, recent...)
	messages = append(messages, current)
	return messages, nil
}
