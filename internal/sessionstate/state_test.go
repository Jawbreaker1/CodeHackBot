package sessionstate

import (
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	want := State{
		Status:   "active",
		BaseURL:  "http://127.0.0.1:1234/v1",
		Model:    "test-model",
		MaxSteps: 3,
		AllowAll: true,
		Packet: ctxpacket.WorkerPacket{
			BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
			SessionFoundation: session.Foundation{Goal: "test", ReportingRequirement: "owasp"},
			RunningSummary:    "summary",
		},
		Summary: "done",
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != Version || got.Status != want.Status || got.Model != want.Model || got.Packet.SessionFoundation.Goal != want.Packet.SessionFoundation.Goal {
		t.Fatalf("loaded state mismatch: %#v", got)
	}
	if got.Summary != want.Summary {
		t.Fatalf("Summary = %q", got.Summary)
	}
}
