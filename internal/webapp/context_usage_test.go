package webapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestLastCoordinatorRequestContextRestoresMeasuredPacket(t *testing.T) {
	root := t.TempDir()
	messages := []llmclient.Message{{Role: "system", Content: "system frame"}, {Role: "user", Content: "bounded planning request"}}
	data, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "coordinator-01-request.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	usage := lastCoordinatorRequestContext(root, 128<<10)
	if usage.UsedBytes != len(messages[0].Content)+len(messages[1].Content) || usage.LimitBytes != 128<<10 {
		t.Fatalf("restored coordinator packet usage = %+v", usage)
	}
	usagePath := filepath.Join(root, "coordinator-01-usage.json")
	if err := os.WriteFile(usagePath, []byte(`{"request_input_bytes":39,"provider_input_tokens":15}`), 0600); err != nil {
		t.Fatal(err)
	}
	// A token count only belongs to the packet whose exact measured bytes
	// match the saved usage record.
	if restored := lastCoordinatorRequestContext(root, 128<<10); restored.ProviderInputTokens != 0 {
		t.Fatalf("accepted mismatched token record: %+v", restored)
	}
	record, _ := json.Marshal(map[string]int{"request_input_bytes": usage.UsedBytes, "provider_input_tokens": 15})
	if err := os.WriteFile(usagePath, record, 0600); err != nil {
		t.Fatal(err)
	}
	if restored := lastCoordinatorRequestContext(root, 128<<10); restored.ProviderInputTokens != 15 {
		t.Fatalf("lost matching provider token count: %+v", restored)
	}
}

func TestTokenAwareContextWindowUsesProviderCountAfterCompletion(t *testing.T) {
	state := assessment.State{MaxInputBytes: 1000, MaxInputTokens: 100}
	primary, all := agentContextWindows(state, contextWindowView{UsedBytes: 350, LimitBytes: 1000, LimitTokens: 100, ProviderInputTokens: 72}, []workerView{
		{ID: "measured", Phase: "decision_started", ContextUsedBytes: 100, ContextLimitBytes: 1000, ProviderInputTokens: 28},
		{ID: "pending", Phase: "decision_started", ContextUsedBytes: 250, ContextLimitBytes: 1000},
	}, "running", true, false, nil)
	if primary.LimitTokens != 100 || primary.Percent != 72 || len(all) != 3 || all[1].Percent != 28 || all[2].Percent != 25 {
		t.Fatalf("token/byte display did not distinguish completed and pending requests: primary=%+v all=%+v", primary, all)
	}
	chat, _ := agentContextWindows(state, contextWindowView{UsedBytes: 350, LimitBytes: 1000, ProviderInputTokens: 72}, nil, "completed", false, false, nil)
	if chat.LimitTokens != 0 || chat.Percent != 35 {
		t.Fatalf("byte-governed follow-up chat inherited the assessment token limit: %+v", chat)
	}
}
