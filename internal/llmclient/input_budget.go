package llmclient

import "fmt"

// InputBudgetUnits expresses the larger of byte and token pressure on one
// complete request. One million units means the request exactly fills its
// configured input allowance. Token-aware compaction uses the same projection
// order as byte compaction and stops only when both limits fit.
const InputBudgetUnits = 1_000_000

func (c Client) InputPressure(messages []Message) (int, error) {
	bytes := 0
	for _, message := range messages {
		bytes += len(message.Content) + len(message.ReasoningContent)
	}
	pressure := ceilingRatio(bytes, c.InputByteLimit())
	if c.MaxInputTokens > 0 {
		if c.CountInputTokens == nil {
			return 0, fmt.Errorf("model input token limit requires a preflight counter")
		}
		tokens := c.CountInputTokens(messages)
		if tokens <= 0 {
			return 0, fmt.Errorf("model input token counter returned no count")
		}
		pressure = max(pressure, ceilingRatio(tokens, c.MaxInputTokens))
	}
	return pressure, nil
}

func ceilingRatio(used, limit int) int {
	if used <= 0 {
		return 0
	}
	return (used*InputBudgetUnits + limit - 1) / limit
}
