// Package intake contains the model-led conversational intake shared by the
// terminal and browser adapters.
package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

// Draft is the model's proposed assessment. It is still subject to operator
// review and does not grant execution permission.
type Draft struct {
	Goal  string `json:"goal"`
	Scope string `json:"scope"`
}

// Turn is the strict response contract for conversational intake.
type Turn struct {
	Reply    string `json:"reply"`
	Proposal *Draft `json:"proposal"`
}

// Conversation retains a bounded model-facing transcript for one intake.
type Conversation struct {
	messages []llmclient.Message
}

const systemPrompt = `You are BirdHackBot's conversational assessment orchestrator.
Answer ordinary questions about the harness, models, workers, and workflow naturally. Do not run tools or claim to have inspected a target during intake. When the operator wants a security assessment, ask for missing objective, exact targets, allowed actions, and exclusions. Preserve the operator's stated boundaries and never invent authorization or targets.
Return exactly one JSON object: {"reply":"natural language response", "proposal":null}.
Use proposal:null for discussion, clarification, corrections, uncertainty, or canceled work. When the operator has provided a concrete objective and exact scope, set proposal to {"goal":"objective", "scope":"exact targets, allowed actions, and exclusions"}. A proposal is shown for explicit operator confirmation; it is never execution permission. The application owns approvals and the start confirmation.`

// Turn sends one operator message to the model and validates the response.
func (c *Conversation) Turn(ctx context.Context, client llmclient.Client, input string) (Turn, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Turn{}, fmt.Errorf("intake message is required")
	}
	c.add("user", input)
	messages := make([]llmclient.Message, 0, len(c.messages)+1)
	messages = append(messages, llmclient.Message{Role: "system", Content: systemPrompt})
	messages = append(messages, c.messages...)
	raw, err := client.ChatStructured(ctx, messages)
	if err != nil {
		return Turn{}, fmt.Errorf("intake response: %w", err)
	}
	turn, err := DecodeTurn(raw)
	if err != nil {
		return Turn{}, err
	}
	c.add("assistant", turn.Reply)
	return turn, nil
}

// Messages returns a copy suitable for diagnostics or a UI read model.
func (c *Conversation) Messages() []llmclient.Message {
	return append([]llmclient.Message(nil), c.messages...)
}

// DecodeTurn validates the model's machine-readable intake response.
func DecodeTurn(raw string) (Turn, error) {
	var turn Turn
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &turn); err != nil {
		return Turn{}, fmt.Errorf("intake protocol response is invalid: %w", err)
	}
	turn.Reply = strings.TrimSpace(turn.Reply)
	if turn.Reply == "" {
		return Turn{}, fmt.Errorf("intake response has no reply")
	}
	if turn.Proposal != nil && (strings.TrimSpace(turn.Proposal.Goal) == "" || strings.TrimSpace(turn.Proposal.Scope) == "") {
		return Turn{}, fmt.Errorf("intake proposal must include goal and scope")
	}
	return turn, nil
}

func (c *Conversation) add(role, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	c.messages = append(c.messages, llmclient.Message{Role: role, Content: content})
	if len(c.messages) > 12 {
		c.messages = c.messages[len(c.messages)-12:]
	}
}
