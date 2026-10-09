package assessment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestCoordinatorPacketIncludesBoundedPriorWorkAsHistory(t *testing.T) {
	state := State{Version: 1, ID: "current", Goal: "Retest the target", Scope: "opensverige.se", Target: "opensverige.se",
		ProjectHistory: &ProjectHistory{Project: "lab", Target: "opensverige.se", Sessions: []PriorSession{{ID: "older", Findings: []PriorFinding{{ID: "older#abc", Title: "Earlier candidate", Status: "candidate"}}}}}}
	var packet coordinatorModelPacket
	if err := json.Unmarshal([]byte(coordinatorPrompt(state)), &packet); err != nil {
		t.Fatal(err)
	}
	if packet.ProjectHistory == nil || packet.ProjectHistory.Sessions[0].Findings[0].ID != "older#abc" {
		t.Fatalf("prior work missing from coordinator context: %+v", packet.ProjectHistory)
	}
	if !strings.Contains(strings.Join(packet.Instructions, " "), "not evidence collected in this run") {
		t.Fatal("prior work is not clearly distinguished from current evidence")
	}
	bounded, err := coordinatorPromptBounded(state, len(coordinatorPrompt(state))-1)
	if err != nil {
		t.Fatal(err)
	}
	var compacted coordinatorModelPacket
	if err := json.Unmarshal([]byte(bounded), &compacted); err != nil {
		t.Fatal(err)
	}
	if compacted.ProjectHistory != nil || compacted.Assessment.Goal != state.Goal || compacted.Assessment.Scope != state.Scope {
		t.Fatalf("historical context displaced current objective under pressure: %+v", compacted)
	}
}

func TestPriorFindingReviewRequiresOwnEvidenceAndChallengeForRemediation(t *testing.T) {
	log := "/work/retest/action.log"
	state := State{Limits: DefaultLimits(), ProjectHistory: &ProjectHistory{Sessions: []PriorSession{{ID: "older", Findings: []PriorFinding{{ID: "older#abc"}}}}},
		Results: []Result{{Task: Task{ID: "retest", Verification: &VerificationRequest{Claim: "old weakness remains", Alternative: "the control now blocks it"}}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{log}}}, Verification: &VerificationResult{Verdict: "supported", AlternativeResult: "control checked", Reason: "fresh comparison", Evidence: []string{log}}}}}
	decision := Decision{Summary: "Retest complete", Complete: true, PriorReviews: []PriorReview{{FindingID: "older#abc", Verdict: "remediated", Reason: "Former behavior was blocked on the retest", ValidationTask: "retest", Evidence: []string{log}}}}
	if err := validateDecision(decision, state); err != nil {
		t.Fatalf("supported retest rejected: %v", err)
	}
	decision.PriorReviews[0].Evidence = []string{"/unrecorded.log"}
	if err := validateDecision(decision, state); err == nil {
		t.Fatal("unrecorded retest evidence was accepted")
	}
	decision.PriorReviews[0].Evidence = []string{log}
	state.Results[0].Verification.Verdict = "inconclusive"
	if err := validateDecision(decision, state); err == nil {
		t.Fatal("remediation was accepted without a supported challenge")
	}
	decision.PriorReviews[0].Verdict = "not_observed"
	if err := validateDecision(decision, state); err != nil {
		t.Fatalf("evidence-backed limited observation rejected: %v", err)
	}
}

func TestCanonicalReportNamesRetestedPriorFindingAndCurrentEvidence(t *testing.T) {
	root := t.TempDir()
	state := State{ID: "current", Goal: "Retest older issue", Scope: "example.test", Status: "completed", Model: "fixture", Limits: DefaultLimits(),
		Plans: []Decision{{Complete: true, Summary: "The retest finished", PriorReviews: []PriorReview{{FindingID: "older#abc", Verdict: "not_observed", Reason: "No former behavior in this bounded check", ValidationTask: "retest", Evidence: []string{"/work/retest/action.log"}}}}}}
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"Follow-up on earlier project findings", "older#abc", "not_observed", "/work/retest/action.log"} {
		if !strings.Contains(string(data), wanted) {
			t.Fatalf("canonical report omitted %q", wanted)
		}
	}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		formatted, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(formatted), "Follow-up on earlier project findings") || !strings.Contains(string(formatted), "older#abc") {
			t.Fatalf("%s report omitted prior-finding retest", format)
		}
	}
}
