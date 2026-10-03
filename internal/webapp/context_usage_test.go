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
}
