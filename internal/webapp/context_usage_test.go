package webapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
