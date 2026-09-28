package assessment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func reportCheck(t *testing.T, r ReportReadiness, id string) ReportCheck {
	t.Helper()
	for _, check := range r.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("missing report check %s", id)
	return ReportCheck{}
}

func TestReportReadinessTracksPendingWorkWithoutInventingGaps(t *testing.T) {
	state := State{Goal: "Review one service", Scope: "192.0.2.1", Status: "running", StartedAt: time.Now(), Plans: []Decision{{Tasks: []Task{{ID: "inspect"}}}}}
	r := AssessReportReadiness(state)
	if r.Status != "collecting" || len(r.Attention()) != 0 {
		t.Fatalf("running work was labeled deficient: %+v", r)
	}
	if checks := reportAttentionChecks(state); len(checks) != 0 {
		t.Fatalf("normal pending work should not consume coordinator context: %v", checks)
	}
	for _, id := range []string{"window", "outcomes", "conclusion"} {
		if got := reportCheck(t, r, id).Status; got != "pending" {
			t.Fatalf("%s status = %s, want pending", id, got)
		}
	}
}

func TestReportReadinessUsesSavedOutcomeAndActualFindingEvidence(t *testing.T) {
	ref := filepath.Join(t.TempDir(), "inspect.log")
	if err := os.WriteFile(ref, []byte("observed response"), 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Minute)
	state := State{Goal: "Review one service", Scope: "192.0.2.1", Status: "completed", StartedAt: start, FinishedAt: start.Add(time.Minute),
		Plans:   []Decision{{Tasks: []Task{{ID: "inspect"}}}, {Complete: true, Summary: "One candidate needs review", Findings: []Finding{{Title: "Observed exposure", Status: "candidate", Impact: "May reveal data", Steps: []string{"Repeat the request"}, Evidence: []string{ref}, Remediation: []string{"Limit access"}}}}},
		Results: []Result{{Task: Task{ID: "inspect"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{ref}}}}},
	}
	r := AssessReportReadiness(state)
	if r.Status != "ready_for_review" || len(r.Attention()) != 0 {
		t.Fatalf("recorded draft should be ready for professional review: %+v", r)
	}
	if got := reportCheck(t, r, "findings").Status; got != "recorded" {
		t.Fatalf("finding evidence status = %s", got)
	}
	if err := os.Remove(ref); err != nil {
		t.Fatal(err)
	}
	r = AssessReportReadiness(state)
	if r.Status != "needs_attention" || reportCheck(t, r, "findings").Status != "needs_attention" {
		t.Fatalf("missing evidence was not surfaced: %+v", r)
	}
	if checks := reportAttentionChecks(state); len(checks) != 1 || checks[0] != "findings" {
		t.Fatalf("coordinator did not receive the actionable missing field: %v", checks)
	}
	text, err := RenderFormattedReport(state, OWASPReport)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "Finding evidence (needs_attention)") || !strings.Contains(string(text), "accessible, registered evidence files") {
		t.Fatalf("report omitted the visible evidence gap: %s", text)
	}
}

func TestReportReadinessRequiresReconciledConclusionAndLimitations(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	state := State{Goal: "Review one service", Scope: "192.0.2.1", Status: "completed_with_gaps", StartedAt: start, FinishedAt: start.Add(time.Minute),
		Plans: []Decision{{Tasks: []Task{{ID: "inspect"}}}}, Results: []Result{{Task: Task{ID: "inspect"}, Status: "blocked"}},
	}
	r := AssessReportReadiness(state)
	if reportCheck(t, r, "limitations").Status != "needs_attention" || reportCheck(t, r, "conclusion").Status != "needs_attention" {
		t.Fatalf("unreconciled run was marked complete: %+v", r)
	}
	state.Plans = append(state.Plans, Decision{Complete: true, Summary: "Inspection stopped", Gaps: []string{"The service was not inspected."}})
	r = AssessReportReadiness(state)
	if reportCheck(t, r, "limitations").Status != "recorded" || reportCheck(t, r, "conclusion").Status != "recorded" {
		t.Fatalf("explicit gaps and conclusion were not recognized: %+v", r)
	}
}
