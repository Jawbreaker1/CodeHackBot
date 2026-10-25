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

func TestCoordinatorRunsFourIndependentTasksAndPreservesResults(t *testing.T) {
	var started atomic.Int32
	var releaseOnce sync.Once
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if len(request.Messages) < 2 {
			t.Error("missing model packet")
			return
		}
		if strings.Contains(request.Messages[1].Content, `"role":"assessment_coordinator"`) {
			reply(w, Decision{Summary: "Inspect four independent fixture partitions", Tasks: []Task{
				{ID: "first", Goal: "Inspect fixture partition A", DoneWhen: "A recorded"},
				{ID: "second", Goal: "Inspect fixture partition B", DoneWhen: "B recorded"},
				{ID: "third", Goal: "Inspect fixture partition C", DoneWhen: "C recorded"},
				{ID: "fourth", Goal: "Inspect fixture partition D", DoneWhen: "D recorded"},
			}})
			return
		}
		if started.Add(1) == 4 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		reply(w, map[string]string{"type": "invalid-fixture-decision"})
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(release) })
	coordinator := testCoordinator(server.URL)
	coordinator.Limits = Limits{Workers: 4, Rounds: 1, Tasks: 4, StepsPerTask: 1, ModelCalls: 16}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state, err := coordinator.Run(ctx, t.TempDir(), "Inspect partitions", "synthetic fixtures only")
	if started.Load() < 4 || len(state.Results) != 4 {
		t.Fatalf("workers did not run concurrently and return separate results: started=%d results=%+v err=%v", started.Load(), state.Results, err)
	}
	for i, id := range []string{"first", "second", "third", "fourth"} {
		if state.Results[i].Task.ID != id {
			t.Fatalf("result %d lost its task identity: %+v", i, state.Results)
		}
	}
}
