package assessment

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestConversationRequestKeepsRecentDialogueAndProtectsCurrentMessage(t *testing.T) {
	prior := []llmclient.Message{
		{Role: "user", Content: strings.Repeat("old discussion ", 400)},
		{Role: "assistant", Content: "The marker is violet-otter."},
	}
	current := llmclient.Message{Role: "user", Content: "What was the marker?"}
	messages, err := ConversationRequest("system", "state", prior, current, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || messages[2].Role != "assistant" || !strings.Contains(messages[2].Content, "violet-otter") || messages[3].Content != current.Content {
		t.Fatalf("recent discussion or current turn was lost: %+v", messages)
	}
	if !strings.Contains(messages[1].Content, "Earlier dialogue was omitted") || len(prior[0].Content) < 4000 {
		t.Fatal("compaction was not marked or changed the durable transcript")
	}
	if _, err := ConversationRequest("system", strings.Repeat("state", 100), nil, current, 32); err == nil {
		t.Fatal("oversized protected state should fail visibly")
	}
}

func TestPlanBriefCarriesPurposeChoicesAndCoverageLimits(t *testing.T) {
	brief := BriefPlan(Decision{
		Phase: "assessment", PlainSummary: "Check access controls because the API exposes record IDs.",
		Review: "Discovery found two API routes, but no vulnerability has been verified.",
		Tasks:  []Task{{ID: "access", Goal: "Compare records across two authorized roles", DoneWhen: "role-specific responses are recorded"}},
		Gaps:   []string{"Session handling has not been tested."},
	})
	if !strings.Contains(brief.Purpose, "because") || len(brief.Tasks) != 1 || !strings.Contains(brief.Tasks[0], "role-specific responses") || len(brief.Gaps) != 1 || !strings.Contains(brief.PreviousResult, "no vulnerability") {
		t.Fatalf("plan brief lost operator guidance: %+v", brief)
	}
}
