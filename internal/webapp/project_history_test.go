package webapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestProjectHistoryKeepsOnlyEarlierRunsOnSameTarget(t *testing.T) {
	root := t.TempDir()
	finished := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	makeRun := func(id, customer, scope string) *run {
		return &run{id: id, customer: customer, root: filepath.Join(root, id), status: "completed", updatedAt: finished,
			state: assessment.State{ID: id, Goal: "Review the public website", Scope: scope, Status: "completed", FinishedAt: finished,
				Plans:   []assessment.Decision{{Findings: []assessment.Finding{{Title: "Missing security policy", Status: "candidate", Severity: "low", Evidence: []string{"/evidence/policy.log"}}}}},
				Results: []assessment.Result{{Task: assessment.Task{ID: "inspect"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"/evidence/policy.log"}}}}}}}
	}
	server := &Server{runs: map[string]*run{
		"older":   makeRun("older", "lab", "Target: the exact hostname `example.test`, including linked pages."),
		"other":   makeRun("other", "lab", "Target: the exact hostname other.example, including linked pages."),
		"foreign": makeRun("foreign", "another-lab", "Target: the exact hostname example.test."),
		"current": makeRun("current", "lab", "Target: the exact hostname example.test."),
	}}
	history := server.projectHistory("lab", "https://EXAMPLE.TEST/", "current")
	if history == nil || history.Target != "example.test" || len(history.Sessions) != 1 || history.Sessions[0].ID != "older" {
		t.Fatalf("wrong bounded project history: %+v", history)
	}
	if len(history.Sessions[0].Findings) != 1 || history.Sessions[0].Findings[0].ID == "" || history.Sessions[0].Findings[0].EvidenceRef != "/evidence/policy.log" {
		t.Fatalf("prior finding lost source identity: %+v", history.Sessions[0].Findings)
	}
	if got := legacyHostname("Review the local file secret.zip."); got != "" {
		t.Fatalf("filename misidentified as hostname: %q", got)
	}
	if got := legacyHostname("Test exactly the hostname example.test. The named hostname. Allowed actions are read-only."); got != "example.test" {
		t.Fatalf("descriptive hostname prose obscured an explicit target: %q", got)
	}
	if got := canonicalTarget("https://example.test:8443/admin/"); got != "https://example.test:8443/admin" {
		t.Fatalf("path-specific service was merged into host identity: %q", got)
	}
}

func TestCustomerAnalysisGroupsExplicitTargetAcrossDifferentScopeProse(t *testing.T) {
	first := assessment.State{ID: "first", Scope: "Test exactly hostname example.test with DNS only", Status: "completed", FinishedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), Plans: []assessment.Decision{{Findings: []assessment.Finding{{Title: "First observation", Status: "candidate"}}}}}
	second := assessment.State{ID: "second", Scope: "Target: hostname `example.test`, with HTTP checks", Status: "completed", FinishedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), Plans: []assessment.Decision{{Findings: []assessment.Finding{{Title: "Later observation", Status: "candidate"}}}}}
	view := buildCustomerAnalysis("lab", []analysisView{buildAnalysis("first", "lab", first, nil), buildAnalysis("second", "lab", second, nil)})
	if view.Sessions[0].Target != "example.test" || view.Sessions[1].Target != "example.test" || view.Sessions[1].UpdatedAt.IsZero() {
		t.Fatalf("target chronology missing: %+v", view.Sessions)
	}
	foundTarget := false
	for _, group := range view.Correlations {
		if group.Label == "Same target: example.test" && len(group.Matches) == 2 {
			foundTarget = true
		}
	}
	if !foundTarget {
		t.Fatalf("target overlap missing despite different scope prose: %+v", view.Correlations)
	}
}

func TestCustomerAnalysisRetestDoesNotEraseOriginalEvidence(t *testing.T) {
	first := assessment.State{Scope: "hostname example.test", Status: "completed", FinishedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), Plans: []assessment.Decision{{Findings: []assessment.Finding{{Title: "Access control flaw", Status: "candidate", Evidence: []string{"/older/action.log"}}}}}}
	firstView := buildAnalysis("first", "lab", first, nil)
	id := firstView.Findings[0].ID
	second := assessment.State{Scope: "hostname example.test", Status: "completed", FinishedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), Plans: []assessment.Decision{{PriorReviews: []assessment.PriorReview{{FindingID: id, Verdict: "remediated", Reason: "Fresh control check blocked the former behavior", ValidationTask: "retest", Evidence: []string{"/new/action.log"}}}}}}
	combined := buildCustomerAnalysis("lab", []analysisView{firstView, buildAnalysis("second", "lab", second, nil)})
	if len(combined.Findings) != 1 || combined.Findings[0].LatestReview == nil || combined.Findings[0].LatestReview.Verdict != "remediated" || combined.Findings[0].Evidence[0] != "/older/action.log" {
		t.Fatalf("retest erased or failed to link original evidence: %+v", combined.Findings)
	}
	if combined.Risk.Remediated != 1 || combined.Risk.Candidates != 0 || len(combined.PriorReviews) != 1 {
		t.Fatalf("retested finding still counted as active risk: risk=%+v reviews=%+v", combined.Risk, combined.PriorReviews)
	}
}

func TestNewWebRunSendsPriorSessionToCoordinatorWithoutReusingItsEvidence(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "stop after recording request", http.StatusBadRequest)
	}))
	defer model.Close()
	server := NewServer(Config{RepoRoot: t.TempDir(), LLM: llmclient.Client{BaseURL: model.URL, Model: "fixture"}})
	prior, err := server.newRun("lab", "Inspect example.test", "Target: hostname example.test")
	if err != nil {
		t.Fatal(err)
	}
	prior.status = "completed"
	prior.state = assessment.State{ID: prior.id, Goal: prior.goal, Scope: prior.scope, Status: "completed", FinishedAt: time.Now().Add(-time.Hour),
		Plans:   []assessment.Decision{{Findings: []assessment.Finding{{Title: "Earlier missing control", Status: "candidate", Evidence: []string{"/prior/observation.log"}}}}},
		Results: []assessment.Result{{Task: assessment.Task{ID: "inspect"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"/prior/observation.log"}}}}}}
	current, err := server.newRun("lab", "Retest example.test", "Target: hostname example.test")
	if err != nil {
		t.Fatal(err)
	}
	current.state.Target = "example.test"
	server.runAssessment(context.Background(), current)
	data, err := os.ReadFile(filepath.Join(current.root, "coordinator-01-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var messages []llmclient.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || !strings.Contains(messages[1].Content, `"project_history"`) || !strings.Contains(messages[1].Content, "Earlier missing control") || !strings.Contains(messages[1].Content, `"recorded_evidence":{}`) {
		t.Fatalf("prior work was not separated from current evidence: %s", messages[1].Content)
	}
}
