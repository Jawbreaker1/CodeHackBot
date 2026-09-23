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
