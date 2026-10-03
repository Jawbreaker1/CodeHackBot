package assessment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverInterruptedBatchRetainsFinishedResultAndMarksMissingWorker(t *testing.T) {
	root := t.TempDir()
	first := Task{ID: "first", Goal: "inspect fixture", DoneWhen: "record result"}
	second := Task{ID: "second", Goal: "check another fixture", DoneWhen: "record result"}
	state := State{Plans: []Decision{{Tasks: []Task{first, second}, ApprovedTaskIDs: []string{first.ID, second.ID}}}}
	result := Result{Task: first, Status: "done", Summary: "Observed the fixture", Evidence: nil}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tasks", first.ID, "result.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if !recoverInterruptedBatch(root, &state) || len(state.Results) != 2 || state.Results[0].Status != "done" || state.Results[0].Summary != result.Summary || state.Results[1].Status != "blocked" || state.Results[1].Task.ID != second.ID {
		t.Fatalf("batch recovery lost a result or repeated unfinished work: %+v", state.Results)
	}
	if recoverInterruptedBatch(root, &state) || len(state.Results) != 2 {
		t.Fatalf("repeated recovery duplicated task results: %+v", state.Results)
	}
}

func TestRecoverInterruptedBatchDoesNotReviveSkippedTasks(t *testing.T) {
	state := State{Plans: []Decision{{Tasks: []Task{{ID: "approved"}, {ID: "skipped"}}, ApprovedTaskIDs: []string{"approved"}, SkippedTaskIDs: []string{"skipped"}}}}
	if !recoverInterruptedBatch(t.TempDir(), &state) || len(state.Results) != 1 || state.Results[0].Task.ID != "approved" || state.Results[0].Status != "blocked" {
		t.Fatalf("skipped task was revived: %+v", state.Results)
	}
}

func TestResumePlansFromRecoveredWorkerResultsWithoutReplayingTasks(t *testing.T) {
	root := t.TempDir()
	finished := Task{ID: "finished", Goal: "inspect synthetic fixture", DoneWhen: "evidence saved"}
	interrupted := Task{ID: "interrupted", Goal: "review another fixture", DoneWhen: "evidence saved"}
	result := Result{Task: finished, Status: "done", Summary: "The bounded fixture inspection finished."}
	if err := saveJSON(filepath.Join(root, "tasks", finished.ID, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	var request string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		for _, message := range input.Messages {
			request += message.Content
		}
		reply(w, Decision{Summary: "Review saved results before any further work", Complete: true, Gaps: []string{"The interrupted fixture review was not completed."}})
	}))
	defer server.Close()
	coordinator := testCoordinator(server.URL)
	state, err := coordinator.RunState(context.Background(), root, State{
		Version: 1, Goal: "review synthetic fixtures", Scope: "local fixtures only", Limits: DefaultLimits(),
		Plans: []Decision{{Summary: "Inspect two fixtures", Tasks: []Task{finished, interrupted}, ApprovedTaskIDs: []string{finished.ID, interrupted.ID}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Results) != 2 || state.Results[0].Status != "done" || state.Results[1].Status != "blocked" || state.Status != "completed_with_gaps" {
		t.Fatalf("resume discarded the saved result or replayed work: %+v", state)
	}
	if !strings.Contains(request, "The bounded fixture inspection finished") || !strings.Contains(request, "interrupted before a completed result") {
		t.Fatal("the coordinator did not receive both the completed and interrupted worker outcomes")
	}
}
