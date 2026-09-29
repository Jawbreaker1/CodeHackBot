package context_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/sessionstate"
)

// Exercise the projection/persistence boundary repeatedly. The fixture is data
// only: it neither invokes a model nor executes any command or network request.
func TestContextLifecycleLongSession(t *testing.T) {
	const turns = 48
	const tightBudget = 18000
	root := t.TempDir()
	states := []sessionstate.State{lifecycleState("worker-amber"), lifecycleState("worker-indigo")}
	compacted, offloaded := false, false
	for turn := 1; turn <= turns; turn++ {
		for i := range states {
			state := &states[i]
			id := []string{"worker-amber", "worker-indigo"}[i]
			otherID := []string{"worker-indigo", "worker-amber"}[i]
			appendLifecycleTurn(&state.Packet, id, turn)
			before := lifecycleJSON(t, *state)
			full, err := state.Packet.ModelView(len(state.Packet.Render()) + 4096)
			if err != nil {
				t.Fatalf("%s turn %d, full view: %v", id, turn, err)
			}
			assertLifecycleContract(t, state.Packet, full)
			for _, field := range []struct {
				name      string
				got, want any
			}{
				{"results", full.RelevantRecentResults, state.Packet.RelevantRecentResults},
				{"conversation", full.RecentConversation, state.Packet.RecentConversation},
				{"plan revisions", full.PlanHistory, state.Packet.PlanHistory},
			} {
				if !reflect.DeepEqual(field.got, field.want) {
					t.Fatalf("%s turn %d: %s changed despite headroom", id, turn, field.name)
				}
			}
			if len(full.ContextNotes) != 0 || full.OffloadedResultCount != 0 {
				t.Fatalf("%s turn %d: compaction leaked from a previous request", id, turn)
			}

			// A smaller request allowance recurs at checkpoint turns; ordinary
			// turns use the full packet again. This catches sticky compaction.
			requestBudget := len(state.Packet.Render()) + 4096
			if turn%6 == 0 {
				requestBudget = tightBudget
			}
			bounded, err := state.Packet.ModelView(requestBudget)
			if err != nil {
				t.Fatalf("%s turn %d, bounded view: %v", id, turn, err)
			}
			assertLifecycleContract(t, state.Packet, bounded)
			if len(bounded.Render()) > requestBudget {
				t.Fatalf("%s turn %d: request exceeds its budget", id, turn)
			}
			if len(full.Render()) > requestBudget {
				compacted = true
				if len(bounded.ContextNotes) == 0 {
					t.Fatalf("%s turn %d: shortened context lacks an omission notice", id, turn)
				}
			}
			offloaded = offloaded || bounded.OffloadedResultCount > 0
			if len(bounded.RelevantRecentResults)+bounded.OffloadedResultCount != turn {
				t.Fatalf("%s turn %d: an observation disappeared without being accounted for", id, turn)
			}
			for _, view := range []ctxpacket.WorkerPacket{full, bounded} {
				if strings.Contains(view.Render(), otherID) {
					t.Fatalf("%s turn %d: another worker's context leaked into this request", id, turn)
				}
				assertLifecycleReferences(t, state.Packet, view)
			}

			// UI/debug consumers may modify returned views. Nested maps and slices
			// must not alias either worker's durable state.
			bounded.BehaviorFrame.Parameters["scope"] = "view-only scope change"
			if bounded.PlanState.StepPurposes != nil {
				bounded.PlanState.StepPurposes[bounded.PlanState.ActiveStep] = "view-only purpose"
			}
			bounded.LatestExecutionResult.LogRefs[0] = "view-only reference"
			bounded.RecentConversation[len(bounded.RecentConversation)-1] = "view-only conversation"
			if !bytes.Equal(before, lifecycleJSON(t, *state)) {
				t.Fatalf("%s turn %d: projection mutated authoritative state", id, turn)
			}

			if turn%6 == 0 {
				path := filepath.Join(root, id, "session.json")
				if err := sessionstate.Save(path, *state); err != nil {
					t.Fatal(err)
				}
				loaded, err := sessionstate.Load(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, lifecycleJSON(t, loaded)) {
					t.Fatalf("%s turn %d: save/resume changed authoritative state", id, turn)
				}
				*state = loaded
			}
			restored, err := state.Packet.ModelView(len(state.Packet.Render()) + 4096)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(full, restored) {
				t.Fatalf("%s turn %d: a roomy request did not recover the original context", id, turn)
			}
		}
	}
	if !compacted || !offloaded {
		t.Fatal("fixture did not exercise both context shortening and result offloading")
	}
	for _, state := range states {
		if len(state.Packet.RelevantRecentResults) != turns || len(state.Packet.RecentConversation) != 2*turns || len(state.Packet.PlanHistory) != turns/4 {
			t.Fatal("a long session lost accumulated observations, conversation, or plan revisions")
		}
	}
}

func TestContextLifecycleLatestOperatorSurvivesAssistantTail(t *testing.T) {
	for _, prefix := range []string{"User: ", "Operator answer: "} {
		t.Run(strings.TrimSpace(prefix), func(t *testing.T) {
			state := lifecycleState("worker-amber")
			for turn := 1; turn <= 48; turn++ {
				appendLifecycleTurn(&state.Packet, "worker-amber", turn)
			}
			instruction := prefix + "CURRENT_OPERATOR_DIRECTION: compare only the two recorded sections; keep the original labels."
			for _, entry := range []string{instruction, "Assistant: I will compare those recorded sections and preserve their labels."} {
				state.Packet.RecentConversation, state.Packet.OlderConversationSummary = ctxpacket.AppendConversation(state.Packet.RecentConversation, state.Packet.OlderConversationSummary, entry)
			}
			view, err := state.Packet.ModelView(18000)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(view.Render(), instruction) {
				t.Fatal("context pressure removed the latest operator instruction because an assistant message followed it")
			}
		})
	}
}

func TestContextLifecycleManyPlanRevisionsFitFixedBudget(t *testing.T) {
	state := lifecycleState("worker-amber")
	for turn := 1; turn <= 400; turn++ {
		state.Packet.PlanHistory = append(state.Packet.PlanHistory, ctxpacket.PlanRevision{
			Turn: turn, Plan: ctxpacket.PlanState{Summary: fmt.Sprintf("revision-%03d", turn)},
		})
	}
	state.Packet.PlanState = ctxpacket.PlanState{Summary: "Current decision still concerns the fixture", ActiveStep: "current-step"}
	before := lifecycleJSON(t, state)
	view, err := state.Packet.ModelView(12000)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Render()) > 12000 || !reflect.DeepEqual(view.PlanState, state.Packet.PlanState) || len(view.PlanHistory) == 0 || view.PlanHistory[len(view.PlanHistory)-1].Turn != 400 {
		t.Fatal("long plan history displaced the current plan or exceeded the request budget")
	}
	if len(view.PlanHistory)+view.OffloadedPlanCount != 400 {
		t.Fatal("historical revisions were lost without an omission count")
	}
	if !bytes.Equal(before, lifecycleJSON(t, state)) {
		t.Fatal("projection mutated the saved plan history")
	}
}

func TestContextLifecyclePinnedEvidencePrecedesUnpinnedHistory(t *testing.T) {
	state := lifecycleState("worker-amber")
	important := ctxpacket.ExecutionResult{
		Action: "important recorded observation", OutputEvidence: strings.Repeat("supporting detail ", 80) + "DECISIVE_OBSERVATION",
		LogRefs: []string{"/fixture/worker-amber/important.log"},
	}
	state.Packet.RelevantRecentResults = append(state.Packet.RelevantRecentResults, important)
	state.Packet.PinnedResultRefs = append(state.Packet.PinnedResultRefs, important.LogRefs[0])
	for turn := 0; turn < 25; turn++ {
		state.Packet.RelevantRecentResults = append(state.Packet.RelevantRecentResults, ctxpacket.ExecutionResult{
			Action: fmt.Sprintf("routine-%d", turn), OutputEvidence: strings.Repeat("routine observation ", 75),
			LogRefs: []string{fmt.Sprintf("/fixture/worker-amber/routine-%d.log", turn)},
		})
	}
	view, err := state.Packet.ModelView(12000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Render(), "DECISIVE_OBSERVATION") {
		t.Fatal("pinned decisive observation was shortened before routine history")
	}
	if view.OffloadedResultCount == 0 {
		t.Fatal("fixture did not put pressure on unpinned history")
	}
}

func lifecycleState(id string) sessionstate.State {
	p := ctxpacket.NewInitialWorkerPacket(
		behavior.Frame{SystemPrompt: "Summarize recorded synthetic observations.", AgentsText: "Preserve source references.", RuntimeMode: "worker", Parameters: map[string]string{"scope": id + "-records"}},
		session.Foundation{Goal: id + "-goal-initial", ReportingRequirement: "cite recorded observations"},
		"/fixture/"+id, "synthetic-model", "per_action", 200,
	)
	p.RecentConversation = []string{}
	p.CapabilityInputs = []string{"synthetic record comparison"}
	return sessionstate.State{Version: sessionstate.Version, Status: "active", Model: "synthetic-model", MaxSteps: 200, Packet: p}
}

func appendLifecycleTurn(p *ctxpacket.WorkerPacket, id string, turn int) {
	marker := fmt.Sprintf("%s-turn-%02d", id, turn)
	phase := (turn-1)/12 + 1
	p.SessionFoundation.Goal = fmt.Sprintf("%s-goal-phase-%d", id, phase)
	p.BehaviorFrame.Parameters["scope"] = fmt.Sprintf("%s-scope-phase-%d", id, phase)
	p.OperatorState.ScopeState = p.BehaviorFrame.Parameters["scope"]
	p.CurrentStep.Objective = marker + "-compare-recorded-observations"
	p.Budget.Used = turn
	for _, entry := range []string{
		"Assistant question: " + marker + "-which recorded section matters?",
		"Operator answer: " + marker + "-compare the current section.\n  Preserve indentation and åäö.",
	} {
		p.RecentConversation, p.OlderConversationSummary = ctxpacket.AppendConversation(p.RecentConversation, p.OlderConversationSummary, entry)
	}
	result := ctxpacket.ExecutionResult{
		Action: marker + "-observation", ActualExec: marker + "-synthetic-record", ExitStatus: "0",
		StartedAt: time.Date(2026, 1, 1, 0, turn, 0, 0, time.UTC), FinishedAt: time.Date(2026, 1, 1, 0, turn, 1, 0, time.UTC),
		OutputSummary: marker + "-summary", OutputEvidence: marker + "-evidence\n" + strings.Repeat("recorded datum åäö 🐦\n", 140) + marker + "-end",
		LogRefs:      []string{"/fixture/" + marker + ".json", "/fixture/" + marker + ".stdout"},
		ArtifactRefs: []string{"/fixture/" + marker + "-table.json"},
	}
	p.LatestExecutionResult = result
	p.RelevantRecentResults = append([]ctxpacket.ExecutionResult{result}, p.RelevantRecentResults...)
	if turn%4 == 0 {
		step := marker + "-step"
		p.PlanState = ctxpacket.PlanState{Mode: "compare", WorkerGoal: id + "-comparison-assignment", Summary: marker + "-plan", Steps: []string{step}, ActiveStep: step, StepPurposes: map[string]string{step: marker + "-purpose"}, ReplanConditions: []string{marker + "-missing-record"}}
		p.PlanHistory = append(p.PlanHistory, ctxpacket.PlanRevision{Turn: turn, AfterExecutionLog: result.LogRefs[0], Plan: p.PlanState})
	}
}

func assertLifecycleContract(t *testing.T, original, view ctxpacket.WorkerPacket) {
	t.Helper()
	if original.SessionFoundation != view.SessionFoundation || original.BehaviorFrame.Parameters["scope"] != view.BehaviorFrame.Parameters["scope"] || original.OperatorState.ScopeState != view.OperatorState.ScopeState {
		t.Fatal("current goal or scope changed in model context")
	}
	if !reflect.DeepEqual(original.CurrentStep, view.CurrentStep) || !reflect.DeepEqual(original.PlanState, view.PlanState) || original.Budget != view.Budget {
		t.Fatal("current step, plan, or consumed budget changed in model context")
	}
	latestInput := original.RecentConversation[len(original.RecentConversation)-1]
	if !strings.Contains(view.Render(), latestInput) {
		t.Fatal("latest operator direction is missing from model context")
	}
	if !reflect.DeepEqual(original.LatestExecutionResult.LogRefs, view.LatestExecutionResult.LogRefs) || original.LatestExecutionResult.ExitStatus != view.LatestExecutionResult.ExitStatus {
		t.Fatal("latest observation lost its outcome or source references")
	}
	if original.LatestExecutionResult.OutputSummary != view.LatestExecutionResult.OutputSummary {
		t.Fatal("the short current observation summary changed in model context")
	}
}

func assertLifecycleReferences(t *testing.T, original, view ctxpacket.WorkerPacket) {
	t.Helper()
	byRef := make(map[string]ctxpacket.ExecutionResult, len(original.RelevantRecentResults))
	for _, result := range original.RelevantRecentResults {
		byRef[result.LogRefs[0]] = result
	}
	for _, result := range view.RelevantRecentResults {
		if len(result.LogRefs) == 0 {
			t.Fatal("retained observation has no source reference")
		}
		saved, ok := byRef[result.LogRefs[0]]
		if !ok || !reflect.DeepEqual(saved.LogRefs, result.LogRefs) || saved.ExitStatus != result.ExitStatus || saved.OutputSummary != result.OutputSummary || !saved.StartedAt.Equal(result.StartedAt) || !saved.FinishedAt.Equal(result.FinishedAt) {
			t.Fatal("retained observation has invented or altered provenance")
		}
	}
}

func lifecycleJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
