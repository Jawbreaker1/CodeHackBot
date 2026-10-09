package assessment

import (
	"encoding/json"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestOpenLeadNeedsRecordedEvidenceAndCannotDisappear(t *testing.T) {
	const firstRef = "/fixture/tasks/policy-observation.log"
	const secondRef = "/fixture/tasks/response-observation.log"
	state := State{Limits: Limits{Workers: 2, Rounds: 4, Tasks: 6, StepsPerTask: 4, ModelCalls: 20}, Results: []Result{
		{Task: Task{ID: "policy"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{firstRef}}}},
		{Task: Task{ID: "response"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{secondRef}}}},
	}}
	lead := Lead{ID: "cross-boundary", Hypothesis: "A tenant identifier may bypass a record filter", Evidence: []string{firstRef, secondRef}, NextCheck: "Compare the same synthetic record under two tenant identities"}
	decision := Decision{Summary: "Check the combined observations", Tasks: []Task{{ID: "verify", Goal: "Test the hypothesis", DoneWhen: "Record whether the marker leaves the page"}}, OpenLeads: []Lead{lead}}
	if err := validateDecision(decision, state); err != nil {
		t.Fatal(err)
	}
	bad := decision
	bad.OpenLeads = []Lead{{ID: lead.ID, Hypothesis: lead.Hypothesis, NextCheck: lead.NextCheck, Evidence: []string{"/fixture/not-recorded.log"}}}
	if err := validateDecision(bad, state); err == nil {
		t.Fatal("unrecorded lead evidence was accepted")
	}
	state.Plans = []Decision{decision}
	bad = Decision{Summary: "Done", Complete: true}
	if err := validateDecision(bad, state); err == nil {
		t.Fatal("an unresolved lead disappeared without review")
	}
	bad.OpenLeads = []Lead{lead}
	if err := validateDecision(bad, state); err != nil {
		t.Fatal(err)
	}
	if gaps := ReportGaps(State{Plans: []Decision{bad}}); len(gaps) != 1 || !strings.Contains(gaps[0], lead.Hypothesis) {
		t.Fatal("the unverified lead did not reach the report gaps")
	}
	bad.OpenLeads = nil
	bad.ClosedLeads = []LeadClosure{{ID: lead.ID, Reason: "A later recorded check refuted the proposed flow", Evidence: []string{firstRef}}}
	if err := validateDecision(bad, state); err != nil {
		t.Fatal(err)
	}
}

func TestOpenLeadSurvivesCoordinatorCompaction(t *testing.T) {
	const ref = "/fixture/tasks/early/observation.log"
	state := State{Version: 1, ID: "lead-compaction-fixture", Goal: "Review a synthetic application", Scope: "fixture", Limits: DefaultLimits()}
	for i := 0; i < 9; i++ {
		id := "task-" + string(rune('a'+i))
		result := Result{Task: Task{ID: id}, Status: "done", Summary: strings.Repeat("Long observation. ", 350)}
		if i == 0 {
			result.Evidence = []ctxpacket.ExecutionResult{{LogRefs: []string{ref}, OutputSummary: "The first boundary was observed"}}
			result.LeadHints = []string{"Compare this boundary with later observations before closing coverage."}
		}
		state.Results = append(state.Results, result)
		state.Plans = append(state.Plans, Decision{Summary: strings.Repeat("Prior planning context. ", 150)})
	}
	lead := Lead{ID: "boundary-question", Hypothesis: "Two observed surfaces may share a sensitive data path", Evidence: []string{ref}, NextCheck: "Compare the path using synthetic data"}
	state.Plans[len(state.Plans)-1].OpenLeads = []Lead{lead}
	root := t.TempDir()
	if err := SaveState(root, state); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := coordinatorPromptBounded(state, 50000)
	if err != nil {
		t.Fatal(err)
	}
	var packet coordinatorModelPacket
	if err := json.Unmarshal([]byte(prompt), &packet); err != nil {
		t.Fatal(err)
	}
	if len(packet.Assessment.OpenLeads) != 1 || packet.Assessment.OpenLeads[0].Hypothesis != lead.Hypothesis {
		t.Fatal("compaction lost the open lead")
	}
	if got := packet.RecordedEvidence["task-a"]; len(got) != 1 || got[0] != ref {
		t.Fatal("compaction lost the registered reference behind the lead")
	}
	if len(packet.Assessment.Results[0].LeadHints) != 1 {
		t.Fatal("compaction lost a worker's adjacent lead hint")
	}
	if !strings.Contains(prompt, "compare new worker observations") {
		t.Fatal("coordinator was not instructed to synthesize across workers")
	}
}
