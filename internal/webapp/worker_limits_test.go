package webapp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
)

func TestAssessmentStartsWithSelectedWorkerLimit(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer model.Close()
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), DefaultProfile: "daybreak", Profiles: []ModelProfile{{ID: "daybreak", Provider: "subscription", BaseURL: model.URL + "/v1", Model: "fixture"}}})
	draft, err := server.newIntake()
	if err != nil {
		t.Fatal(err)
	}
	draft.mu.Lock()
	draft.proposal = &intake.Draft{Goal: "Inspect three independent fixtures", Scope: "synthetic fixtures only"}
	draft.mu.Unlock()
	if _, err := server.startIntake(draft, "lab", "", 11); err == nil {
		t.Fatal("worker count above the model ceiling was accepted")
	}
	view, err := server.startIntake(draft, "lab", "", 6)
	if err != nil {
		t.Fatal(err)
	}
	if view.Limits.Workers != 6 || view.Limits.Tasks < 24 || view.Limits.ModelCalls < 384 {
		t.Fatalf("selected limit did not reach assessment state: %+v", view.Limits)
	}
	current := server.getRun(view.ID)
	current.mu.RLock()
	done, cancel := current.done, current.cancel
	current.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("assessment did not stop")
	}
	restored := NewServer(server.config).getRun(view.ID)
	if restored == nil || restored.state.Limits.Workers != 6 {
		t.Fatal("selected worker limit was not restored with the session")
	}
}
