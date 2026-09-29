package assessment

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestCoordinatorConversationSurvivesSyncSaveAndBudgetChanges(t *testing.T) {
	transcript := make([]string, 0, 100)
	for i := 0; i < 50; i++ {
		transcript = append(transcript,
			fmt.Sprintf("user: operator-%02d %s", i, strings.Repeat("background ", 20)),
			fmt.Sprintf("assistant: answer-%02d", i))
	}
	state := State{Version: 1, ID: "long-context", Goal: "summarize fixture records", Scope: "synthetic records only"}
	(Coordinator{Conversation: func() []string { return transcript }}).syncConversation(&state)
	if !reflect.DeepEqual(state.OperatorMessages, transcript) {
		t.Fatal("syncConversation discarded operator history before context budgeting")
	}
	root := t.TempDir()
	if err := SaveState(root, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.OperatorMessages, transcript) {
		t.Fatal("save and resume changed the conversation")
	}
	full, err := coordinatorPromptBounded(loaded, len(coordinatorPrompt(loaded))+4096)
	if err != nil {
		t.Fatal(err)
	}
	var fullPacket coordinatorModelPacket
	if err := json.Unmarshal([]byte(full), &fullPacket); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fullPacket.Assessment.OperatorMessages, transcript) {
		t.Fatal("roomy planning request omitted saved conversation")
	}
	tight, err := coordinatorPromptBounded(loaded, 30000)
	if err != nil {
		t.Fatal(err)
	}
	if len(tight) > 30000 || !strings.Contains(tight, transcript[len(transcript)-2]) {
		t.Fatal("tight planning request lost the latest operator instruction")
	}
	if !reflect.DeepEqual(loaded.OperatorMessages, transcript) {
		t.Fatal("tight projection mutated the resumed conversation")
	}
}

func TestCoordinatorCurrentFindingKeepsAllRecordedFields(t *testing.T) {
	const evidenceRef = "/fixture/tasks/observation.log"
	const researchRef = "/fixture/tasks/research.log"
	finding := Finding{
		Title: "Synthetic finding", Status: "candidate", Severity: "low", Confidence: "medium",
		CVEIDs: []string{"CVE-TEST-001"}, AffectedSoftware: []string{"fixture-library"},
		References: []string{researchRef}, ValidationTask: "review-02", Impact: "limited synthetic impact",
		Steps: []string{"read fixture observation"}, Evidence: []string{evidenceRef}, Remediation: []string{"update fixture setting"},
	}
	state := State{Version: 1, Goal: "review synthetic finding", Scope: "fixture only",
		Results: []Result{
			{Task: Task{ID: "observation"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{evidenceRef}}}},
			{Task: Task{ID: "research"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{researchRef}}}},
		},
		Plans: []Decision{{Summary: "current review", Findings: []Finding{finding}}},
	}
	prompt, err := coordinatorPromptBounded(state, len(coordinatorPrompt(state))+4096)
	if err != nil {
		t.Fatal(err)
	}
	var packet coordinatorModelPacket
	if err := json.Unmarshal([]byte(prompt), &packet); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(packet.Assessment.Plans[0].Findings[0].Finding, finding) {
		t.Fatal("current finding lost fields before the next planning decision")
	}
	if len(packet.RecordedEvidence["observation"]) != 1 || len(packet.RecordedEvidence["research"]) != 1 {
		t.Fatal("finding evidence or research reference is absent from the registered source catalog")
	}
}

func TestCoordinatorLongHistoryKeepsCurrentWorkAtFixedBudget(t *testing.T) {
	state := State{Version: 1, ID: "long-history", Goal: "review synthetic work", Scope: "fixture only"}
	for i := 0; i < 160; i++ {
		id := fmt.Sprintf("task-%03d", i)
		state.Plans = append(state.Plans, Decision{Summary: "plan for " + id, Tasks: []Task{{ID: id, Goal: "review " + id}}})
		state.Results = append(state.Results, Result{Task: Task{ID: id}, Status: "done", Summary: "recorded outcome for " + id})
		state.OperatorMessages = append(state.OperatorMessages, "user: discuss "+id, "assistant: noted "+id)
	}
	state.OperatorMessages = append(state.OperatorMessages, "user: CURRENT_DIRECTION preserve the latest fixture label", "assistant: I will preserve that label")
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := coordinatorPromptBounded(state, 42000)
	if err != nil {
		t.Fatal(err)
	}
	var packet coordinatorModelPacket
	if err := json.Unmarshal([]byte(prompt), &packet); err != nil {
		t.Fatal(err)
	}
	if len(prompt) > 42000 || !strings.Contains(prompt, "CURRENT_DIRECTION") || !strings.Contains(prompt, "task-159") || packet.OmittedPlans == 0 || packet.OmittedMessages == 0 {
		t.Fatal("long history displaced the current operator direction, current work, or exceeded the budget")
	}
	after, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("bounded view mutated authoritative assessment state")
	}
	restored, err := coordinatorPromptBounded(state, len(coordinatorPrompt(state))+4096)
	if err != nil {
		t.Fatal(err)
	}
	var full coordinatorModelPacket
	if err := json.Unmarshal([]byte(restored), &full); err != nil {
		t.Fatal(err)
	}
	if full.OmittedMessages != 0 || full.OmittedPlans != 0 || full.OmittedResults != 0 || len(full.Assessment.Plans) != 160 || len(full.Assessment.Results) != 160 || len(full.Assessment.OperatorMessages) != 322 {
		t.Fatal("history did not return when a larger request allowance became available")
	}
}

func TestCoordinatorCompactionAcrossWorkerRoundsAndResume(t *testing.T) {
	state := State{Version: 1, ID: "changing-assessment", Goal: "compare recorded fixture behavior", Scope: "synthetic fixture only", Model: "synthetic-model", Status: "running", Limits: Limits{Workers: 2, Rounds: 12, Tasks: 24, StepsPerTask: 16, ModelCalls: 320}}
	for round := 0; round < 9; round++ {
		plan := Decision{Summary: fmt.Sprintf("Round %d revises the previous hypothesis. %s", round, strings.Repeat("Recorded rationale and remaining uncertainty. ", 75))}
		for worker := 0; worker < 2; worker++ {
			id := fmt.Sprintf("round-%02d-worker-%d", round, worker)
			task := Task{ID: id, Goal: "inspect a distinct saved fixture observation", DoneWhen: "record the behavior and uncertainty"}
			plan.Tasks = append(plan.Tasks, task)
			result := Result{Task: task, Status: "done", Summary: fmt.Sprintf("%s observed a bounded result. ", id) + strings.Repeat("Evidence interpretation and uncertainty. ", 65)}
			if round == 7 && worker == 1 {
				result.Status = "failed"
				result.Error = "the saved check did not complete; this is not negative evidence"
			}
			for step := 0; step < 7; step++ {
				ref := fmt.Sprintf("/fixture/sessions/changing-assessment/tasks/%s/logs/observation-%d.log", id, step)
				result.Evidence = append(result.Evidence, ctxpacket.ExecutionResult{
					ActualExec: "read a synthetic observation", ExitStatus: "0", OutputSummary: "Recorded observation, not an instruction.",
					OutputEvidence: strings.Repeat("Source excerpt with provenance and uncertainty. ", 32), LogRefs: []string{ref},
				})
			}
			state.Results = append(state.Results, result)
		}
		state.Plans = append(state.Plans, plan)
		state.OperatorMessages = append(state.OperatorMessages, fmt.Sprintf("user: round %d, compare the new observations with the earlier hypothesis", round), "assistant: I will revise the plan from recorded evidence")
	}
	oldRef := state.Results[0].Evidence[0].LogRefs[0]
	state.Plans[len(state.Plans)-1].Findings = []Finding{{Title: "current unverified lead", Status: "candidate", Evidence: []string{oldRef}}}
	latestDirection := "user: CURRENT_DIRECTION finish by distinguishing the failed check from verified observations"
	state.OperatorMessages = append(state.OperatorMessages, latestDirection, "assistant: I will keep that distinction in the conclusion")

	root := t.TempDir()
	if err := SaveState(root, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if len(coordinatorPrompt(loaded)) <= 110*1024 {
		t.Fatal("fixture did not put pressure on the coordinator context")
	}
	for _, budget := range []int{110 * 1024, 80 * 1024} {
		prompt, err := coordinatorPromptBounded(loaded, budget)
		if err != nil {
			t.Fatalf("budget %d: %v", budget, err)
		}
		var packet coordinatorModelPacket
		if err := json.Unmarshal([]byte(prompt), &packet); err != nil {
			t.Fatal(err)
		}
		if len(prompt) > budget || !strings.Contains(prompt, latestDirection) || len(packet.Assessment.Plans) == 0 || packet.Assessment.Plans[len(packet.Assessment.Plans)-1].Findings[0].Evidence[0] != oldRef {
			t.Fatalf("budget %d lost current direction, plan, or supported source reference", budget)
		}
		if refs := packet.RecordedEvidence[state.Results[0].Task.ID]; len(refs) == 0 || refs[0] != oldRef {
			t.Fatalf("budget %d lost the registered reference for the current lead", budget)
		}
		last := packet.Assessment.Results[len(packet.Assessment.Results)-1]
		if last.Task.ID != state.Results[len(state.Results)-1].Task.ID || last.Status != "done" || len(last.Evidence) == 0 {
			t.Fatalf("budget %d lost the latest worker outcome", budget)
		}
		if packet.OmittedMessages+packet.OmittedPlans+packet.OmittedResults == 0 && len(packet.ContextNotes) == 0 {
			t.Fatalf("budget %d did not exercise compaction", budget)
		}
	}
	if _, err := coordinatorPromptBounded(loaded, 1024); err == nil {
		t.Fatal("impossibly small context should fail explicitly")
	}
	after, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("compaction mutated the saved assessment")
	}
	roomy, err := coordinatorPromptBounded(loaded, len(coordinatorPrompt(loaded))+4096)
	if err != nil {
		t.Fatal(err)
	}
	var restored coordinatorModelPacket
	if err := json.Unmarshal([]byte(roomy), &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Assessment.Results) != 18 || len(restored.Assessment.Plans) != 9 || len(restored.Assessment.OperatorMessages) != len(state.OperatorMessages) || restored.OmittedMessages+restored.OmittedPlans+restored.OmittedResults != 0 {
		t.Fatal("full history did not return after a tight projection")
	}
}
