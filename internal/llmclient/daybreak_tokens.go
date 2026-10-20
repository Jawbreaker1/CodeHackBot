package llmclient

import (
	"sync"

	"github.com/tiktoken-go/tokenizer"
)

// DaybreakTextInputTokens measures text-only chat messages using the o200k
// vocabulary. Six Daybreak Blue bridge probes matched this vocabulary plus
// five provider overhead tokens per message. A 2% margin and 32 fixed tokens
// cover small formatting/tokenizer differences; the byte guard still applies.
// Zero means that a safe text-only count is unavailable (including attachments).
func DaybreakTextInputTokens(messages []Message) int {
	if len(messages) == 0 {
		return 0
	}
	daybreakCodec.mu.Lock()
	defer daybreakCodec.mu.Unlock()
	if daybreakCodec.codec == nil {
		codec, err := tokenizer.Get(tokenizer.O200kBase)
		if err != nil {
			return 0
		}
		daybreakCodec.codec = codec
	}
	total := 0
	for _, message := range messages {
		if len(message.Attachments) > 0 {
			return 0
		}
		count, err := daybreakCodec.codec.Count(message.Content)
		if err != nil {
			return 0
		}
		total += count + 5
		if message.ReasoningContent != "" {
			count, err = daybreakCodec.codec.Count(message.ReasoningContent)
			if err != nil {
				return 0
			}
			total += count
		}
	}
	return total + (total+49)/50 + 32
}

var daybreakCodec struct {
	mu    sync.Mutex
	codec tokenizer.Codec
}

const (
	DaybreakAssessmentInputTokens = 128_000
	DaybreakAssessmentInputBytes  = 1024 * 1024
)

// WithDaybreakAssessmentBudget enables token-driven compaction for the
// assessment's text-only coordinator and workers. Intake, vision, and report
// chats retain their existing byte-only client until attachment-aware token
// counting is available. Unknown subscription models remain byte-only.
func (c Client) WithDaybreakAssessmentBudget() Client {
	if c.Model != "gpt-daybreak-blue-latest" || c.AuthTokenFile == "" || c.MaxInputTokens > 0 {
		return c
	}
	// Only replace the old shipped Daybreak ceiling. A smaller or custom byte
	// ceiling is an operator choice and remains an independent hard guard.
	if c.InputByteLimit() == SubscriptionInputByteLimit {
		c.MaxInputBytes = DaybreakAssessmentInputBytes
	}
	c.MaxInputTokens = DaybreakAssessmentInputTokens
	c.CountInputTokens = DaybreakTextInputTokens
	return c
}
