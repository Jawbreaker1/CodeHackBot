package assessment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestCoordinatorReplansWhenOperatorSpeaksDuringPlanning(t *testing.T) {
	var mu sync.Mutex
	conversation := []string{"user: Review the fixture."}
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if len(request.Messages) < 2 {
			t.Error("missing coordinator packet")
			return
		}
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			reply(w, Decision{Summary: "Outdated conclusion", Complete: true})
			return
		}
		if !strings.Contains(request.Messages[1].Content, "Continue with the revised objective") {
			t.Error("new operator direction was absent from replanning packet")
		}
		reply(w, Decision{Summary: "Updated conclusion", Complete: true})
	}))
	defer server.Close()
	coordinator := testCoordinator(server.URL)
	coordinator.Limits = Limits{Workers: 1, Rounds: 2, Tasks: 2, StepsPerTask: 1, ModelCalls: 4}
	coordinator.Conversation = func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), conversation...)
	}
	initial := State{Version: 1, Goal: "Review fixture", Scope: "synthetic fixture", Status: "incomplete",
		Plans:   []Decision{{Summary: "Initial fixture check", Tasks: []Task{{ID: "first", Goal: "Inspect fixture", DoneWhen: "record observation"}}}},
		Results: []Result{{Task: Task{ID: "first"}, Status: "blocked", Summary: "First method unavailable"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type outcome struct {
		state State
		err   error
	}
	done := make(chan outcome, 1)
	root := t.TempDir()
	go func() {
		state, err := coordinator.RunState(ctx, root, initial)
		done <- outcome{state, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("coordinator never started planning")
	}
	mu.Lock()
	conversation = append(conversation, "user: Continue with the revised objective.")
	mu.Unlock()
	close(release)
	select {
	case result := <-done:
		if result.err != nil || calls.Load() != 2 || len(result.state.Plans) != 2 || result.state.Plans[1].Summary != "Updated conclusion" {
			t.Fatalf("stale proposal was accepted: calls=%d state=%+v err=%v", calls.Load(), result.state, result.err)
		}
	case <-ctx.Done():
		t.Fatal("coordinator did not finish replanning")
	}
}
