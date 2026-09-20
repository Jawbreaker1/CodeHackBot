package guided

import (
	"context"

	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

// The terminal adapter keeps its historical private names while delegating
// protocol and transcript behavior to the shared intake package.
type intakeTurn struct {
	Reply    string
	Proposal *assessmentDraft
}

type intakeConversation struct {
	conversation intake.Conversation
}

func (c *intakeConversation) turn(ctx context.Context, client llmclient.Client, input string) (intakeTurn, error) {
	turn, err := c.conversation.Turn(ctx, client, input)
	if err != nil {
		return intakeTurn{}, err
	}
	return intakeTurn{Reply: turn.Reply, Proposal: draftFromIntake(turn.Proposal)}, nil
}

func decodeIntakeTurn(raw string) (intakeTurn, error) {
	turn, err := intake.DecodeTurn(raw)
	if err != nil {
		return intakeTurn{}, err
	}
	return intakeTurn{Reply: turn.Reply, Proposal: draftFromIntake(turn.Proposal)}, nil
}

func draftFromIntake(draft *intake.Draft) *assessmentDraft {
	if draft == nil {
		return nil
	}
	return &assessmentDraft{Goal: draft.Goal, Scope: draft.Scope}
}
