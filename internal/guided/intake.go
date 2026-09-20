package guided

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

// intakeTurn is a model protocol, not a local classifier. The model owns the
// meaning of the operator's conversation; the runtime only validates the
// response before showing a proposal or continuing the dialogue.
type intakeTurn struct {
	Reply    string           `json:"reply"`
	Proposal *assessmentDraft `json:"proposal"`
}

type intakeConversation struct {
	messages []llmclient.Message
}

const intakeSystemPrompt = `You are BirdHackBot's conversational assessment orchestrator.
Answer ordinary questions about the harness, models, workers, and workflow naturally. Do not run tools or claim to have inspected a target during intake. When the operator wants a security assessment, ask for missing objective, exact targets, allowed actions, and exclusions. Preserve the operator's stated boundaries and never invent authorization or targets.
Return exactly one JSON object: {"reply":"natural language response", "proposal":null}.
Use proposal:null for discussion, clarification, corrections, uncertainty, or canceled work. When the operator has provided a concrete objective and exact scope, set proposal to {"goal":"objective", "scope":"exact targets, allowed actions, and exclusions"}. A proposal is shown for explicit operator confirmation; it is never execution permission. The CLI owns approvals and the start confirmation.`

func (c *intakeConversation) add(role, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	c.messages = append(c.messages, llmclient.Message{Role: role, Content: content})
	if len(c.messages) > 12 {
		c.messages = c.messages[len(c.messages)-12:]
	}
}

func (c *intakeConversation) prompt() []llmclient.Message {
	result := make([]llmclient.Message, 0, len(c.messages)+1)
	result = append(result, llmclient.Message{Role: "system", Content: intakeSystemPrompt})
	return append(result, c.messages...)
}

func (c *intakeConversation) turn(ctx context.Context, client llmclient.Client, input string) (intakeTurn, error) {
	c.add("user", input)
	raw, err := client.ChatStructured(ctx, c.prompt())
	if err != nil {
		return intakeTurn{}, fmt.Errorf("intake response: %w", err)
	}
	turn, err := decodeIntakeTurn(raw)
	if err != nil {
		return intakeTurn{}, err
	}
	turn.Reply = strings.TrimSpace(turn.Reply)
	if turn.Reply == "" {
		return intakeTurn{}, fmt.Errorf("intake response has no reply")
	}
	if turn.Proposal != nil && (strings.TrimSpace(turn.Proposal.Goal) == "" || strings.TrimSpace(turn.Proposal.Scope) == "") {
		return intakeTurn{}, fmt.Errorf("intake proposal must include goal and scope")
	}
	c.add("assistant", turn.Reply)
	return turn, nil
}

func decodeIntakeTurn(raw string) (intakeTurn, error) {
	var turn intakeTurn
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &turn); err != nil {
		return intakeTurn{}, fmt.Errorf("intake protocol response is invalid: %w", err)
	}
	turn.Reply = strings.TrimSpace(turn.Reply)
	if turn.Reply == "" {
		return intakeTurn{}, fmt.Errorf("intake response has no reply")
	}
	if turn.Proposal != nil && (strings.TrimSpace(turn.Proposal.Goal) == "" || strings.TrimSpace(turn.Proposal.Scope) == "") {
		return intakeTurn{}, fmt.Errorf("intake proposal must include goal and scope")
	}
	return turn, nil
}
