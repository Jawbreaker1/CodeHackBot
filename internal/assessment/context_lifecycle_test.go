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
