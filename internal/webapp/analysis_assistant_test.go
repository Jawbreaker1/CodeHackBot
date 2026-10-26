package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

type analysisResearchFunc func(context.Context, []string) ([]analysisResearchSource, error)

func (f analysisResearchFunc) Read(ctx context.Context, urls []string) ([]analysisResearchSource, error) {
	return f(ctx, urls)
}

func TestAnalysisAssistantSeparatesCurrentWebResearchFromSavedResults(t *testing.T) {
	modelCalls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls++
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		prompt := ""
		for _, message := range request.Messages {
			prompt += message.Content
		}
		answer := `{"research_urls":["https://www.php.net/"]}`
		if modelCalls == 2 {
			if !strings.Contains(prompt, `"id":"W1"`) || !strings.Contains(prompt, "not target test evidence") {
				t.Errorf("current source was not separated from assessment evidence: %s", prompt)
			}
			answer = `{"answer":"The current advisory discusses a PHP issue. This assessment did not establish whether the target uses that version.","sources":["W1","P1"]}`
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	searches := 0
	researcher := analysisResearchFunc(func(_ context.Context, urls []string) ([]analysisResearchSource, error) {
		searches++
		if len(urls) != 1 || urls[0] != "https://www.php.net/" {
			t.Errorf("unexpected research URLs %q", urls)
		}
		return []analysisResearchSource{{ID: "W1", Title: "PHP security advisory", URL: "https://www.php.net/", Status: 200, Excerpt: "Security update"}}, nil
	})
	view := analysisView{Kind: "assessment", ID: "run-1", SessionCount: 1}
	answer, _, err := askAnalysisAssistantWithResearch(context.Background(), llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}, view, nil, analysisAssistantRequest{Question: "Why are PHP issues in the news?"}, researcher)
	if err != nil || modelCalls != 2 || searches != 1 || len(answer.Sources) != 2 || answer.Sources[0].Kind != "web" || answer.Sources[1].Kind == "web" {
		t.Fatalf("researched answer: answer=%+v model_calls=%d searches=%d err=%v", answer, modelCalls, searches, err)
	}
}

func TestAnalysisAssistantSearchesPublicIndexAndKeepsTargetClaimsSeparate(t *testing.T) {
	modelCalls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls++
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		answer := `{"research_query":"PHP core"}`
		if modelCalls == 2 {
			prompt := ""
			for _, message := range request.Messages {
				prompt += message.Content
			}
			if !strings.Contains(prompt, "CVE-2026-0003") || !strings.Contains(prompt, "not target test evidence") {
				t.Errorf("search result was not separated from saved target evidence")
			}
			answer = `{"answer":"NVD lists a recent PHP issue. This assessment has no PHP version evidence, so I cannot say the target is affected.","sources":["W1","P1"]}`
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	searches := 0
	researcher := publicPageResearcher{search: func(_ context.Context, query string) ([]intake.VulnerabilityResult, error) {
		searches++
		if query != "PHP core" {
			t.Errorf("unexpected search query %q", query)
		}
		return []intake.VulnerabilityResult{{ID: "CVE-2026-0003", URL: "https://nvd.nist.gov/vuln/detail/CVE-2026-0003", Description: "A PHP component vulnerability", Published: "2026-03-01", Retrieved: time.Now().UTC()}}, nil
	}}
	answer, _, err := askAnalysisAssistantWithResearch(context.Background(), llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}, analysisView{Kind: "assessment", ID: "run-1", SessionCount: 1}, nil, analysisAssistantRequest{Question: "Research recent PHP security issues. Does my target have them?"}, researcher)
	if err != nil || modelCalls != 2 || searches != 1 || len(answer.Sources) != 2 || answer.Sources[0].URL != "https://nvd.nist.gov/vuln/detail/CVE-2026-0003" {
		t.Fatalf("answer=%+v model_calls=%d searches=%d err=%v", answer, modelCalls, searches, err)
	}
}

func TestAnalysisAssistantRefinesEmptyIndexQueryOnce(t *testing.T) {
	modelCalls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelCalls++
		answers := []string{
			`{"research_query":"PHP recent security vulnerabilities"}`,
			`{"research_query":"PHP"}`,
			`{"answer":"NVD lists a PHP-related vulnerability. This saved assessment has no installed-version evidence.","sources":["W1","P1"]}`,
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answers[modelCalls-1]}}}})
	}))
	defer model.Close()
	queries := []string{}
	researcher := publicPageResearcher{search: func(_ context.Context, query string) ([]intake.VulnerabilityResult, error) {
		queries = append(queries, query)
		if query != "PHP" {
			return nil, nil
		}
		return []intake.VulnerabilityResult{{ID: "CVE-2026-0003", URL: "https://nvd.nist.gov/vuln/detail/CVE-2026-0003", Description: "PHP issue", Retrieved: time.Now().UTC()}}, nil
	}}
	answer, _, err := askAnalysisAssistantWithResearch(context.Background(), llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}, analysisView{Kind: "assessment", ID: "run-1", SessionCount: 1}, nil, analysisAssistantRequest{Question: "Research recent PHP issues"}, researcher)
	if err != nil || modelCalls != 3 || len(queries) != 2 || queries[1] != "PHP" || len(answer.Sources) != 2 {
		t.Fatalf("answer=%+v calls=%d queries=%v err=%v", answer, modelCalls, queries, err)
	}
}

func TestConnectedAnalysisAssistantSearchesWithoutRequestFlag(t *testing.T) {
	modelCalls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelCalls++
		answer := `{"research_query":"PHP"}`
		if modelCalls == 2 {
			answer = `{"answer":"A current public advisory describes PHP risks. This saved run does not establish that the target uses PHP.","sources":["W1","P1"]}`
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	searches := 0
	server.analysisResearcher = publicPageResearcher{search: func(_ context.Context, query string) ([]intake.VulnerabilityResult, error) {
		searches++
		if query != "PHP" {
			t.Errorf("unexpected query %q", query)
		}
		return []intake.VulnerabilityResult{{ID: "CVE-2026-0003", URL: "https://nvd.nist.gov/vuln/detail/CVE-2026-0003", Description: "A public advisory", Retrieved: time.Now().UTC()}}, nil
	}}
	current, err := server.newRun("lab", "PHP question", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + current.id + "/analysis/assistant"
	if !getJSON[analysisAssistantView](t, path).ResearchAvailable {
		t.Fatal("connected analysis did not advertise automatic research")
	}
	response := postJSON[analysisAssistantView](t, path, map[string]string{"question": "Why are people worried about PHP lately? Please research the web."})
	if modelCalls != 2 || searches != 1 || len(response.Messages) != 2 || len(response.Messages[1].Sources) != 2 || response.Messages[1].Sources[0].Kind != "web" {
		t.Fatalf("automatic research failed: calls=%d searches=%d view=%+v", modelCalls, searches, response)
	}
}

func TestAnalysisAssistantAirGappedModeNeverSearches(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		prompt := ""
		for _, message := range request.Messages {
			prompt += message.Content
		}
		if !strings.Contains(prompt, "No current web research is available") {
			t.Errorf("offline instruction missing")
		}
		answer := `{"answer":"The saved assessment cannot establish current PHP news.","sources":["P1"]}`
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}, Frame: behavior.Frame{Parameters: map[string]string{"research_mode": "air_gapped"}}})
	server.analysisResearcher = analysisResearchFunc(func(context.Context, []string) ([]analysisResearchSource, error) {
		t.Fatal("air-gapped assistant made a web search")
		return nil, nil
	})
	current, err := server.newRun("lab", "PHP question", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + current.id + "/analysis/assistant"
	initial := getJSON[analysisAssistantView](t, path)
	if initial.ResearchAvailable {
		t.Fatal("air-gapped view advertised web research")
	}
	response := postJSON[analysisAssistantView](t, path, analysisAssistantRequest{Question: "Why are PHP issues in the news?"})
	if len(response.Messages) != 2 || response.Messages[1].Text == "" {
		t.Fatalf("offline response: %+v", response)
	}
}

func TestProjectFollowupCanStartWithoutChoosingAnOldSession(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answer := `{"answer":"The saved review has an open question.","sources":["P1"],"next_step":"Check the open question against the saved evidence."}`
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	client := llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}
	project := analysisView{Kind: "customer", ID: "lab", SessionCount: 1, Sessions: []analysisSessionSummary{{ID: "old-run", Goal: "Earlier review"}}}
	reply, _, err := askAnalysisAssistant(context.Background(), client, project, nil, analysisAssistantRequest{Question: "What next?"})
	if err != nil || reply.NextStep == "" || reply.SessionID != "" {
		t.Fatalf("project follow-up should not require an old run: reply=%+v err=%v", reply, err)
	}
	assessment := analysisView{Kind: "assessment", ID: "old-run", SessionCount: 1, Goal: "Earlier review"}
	reply, _, err = askAnalysisAssistant(context.Background(), client, assessment, nil, analysisAssistantRequest{Question: "What next?"})
	if err != nil || reply.NextStep != "" {
		t.Fatalf("assessment continuation should require its run ID: reply=%+v err=%v", reply, err)
	}
}

func TestAnalysisAssistantExplainsTwoSessionsWithSavedSources(t *testing.T) {
	var secondID string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llmclient.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var prompt strings.Builder
		for _, message := range request.Messages {
			prompt.WriteString(message.Content)
		}
		if !strings.Contains(prompt.String(), "Earlier access check") || !strings.Contains(prompt.String(), "Later access check") || !strings.Contains(prompt.String(), "two to four short sentences") {
			t.Errorf("analysis assistant did not receive both saved runs and plain-language guidance: %s", prompt.String())
		}
		answer := `{"answer":"The first run reported an access problem. The second run raised a new question, but did not prove that the first problem was fixed.","sources":["F1","F2"],"next_step":"Review the first access problem and propose a focused retest.","session_id":"` + secondID + `"}`
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()

	root := t.TempDir()
	server := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	first, err := server.newRun("shared", "First run", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.newRun("shared", "Second run", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	secondID = second.id
	first.state = assessment.State{ID: first.id, Goal: first.goal, Scope: first.scope, Status: "completed", Plans: []assessment.Decision{{Findings: []assessment.Finding{{Title: "Earlier access check", Status: "candidate", Severity: "high", Impact: "A user might see someone else's report.", Remediation: []string{"Check access for every report."}}}}}}
	second.state = assessment.State{ID: second.id, Goal: second.goal, Scope: second.scope, Status: "completed", Plans: []assessment.Decision{{Findings: []assessment.Finding{{Title: "Later access check", Status: "candidate", Severity: "medium", Impact: "The new result still needs confirmation."}}}}}
	first.status, second.status = "completed", "completed"
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/customers/shared/analysis/assistant"
	got := postJSON[analysisAssistantView](t, path, analysisAssistantRequest{Question: "What changed between the runs?"})
	if len(got.Messages) != 2 || len(got.Messages[1].Sources) != 2 || got.Messages[1].SessionID != second.id || got.Messages[1].NextStep == "" {
		t.Fatalf("assistant reply or handoff = %+v", got)
	}
	for _, source := range got.Messages[1].Sources {
		if !strings.Contains(source.URL, "/analysis?customer=shared&finding=") {
			t.Fatalf("finding source does not open its saved record: %+v", source)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "sessions", "shared", analysisAssistantFile)); err != nil || !strings.Contains(string(data), "What changed") {
		t.Fatalf("assistant conversation was not saved: %v", err)
	}
	restored := NewServer(Config{RepoRoot: root, SessionsRoot: filepath.Join(root, "sessions"), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	restoredHTTP := httptest.NewServer(restored)
	defer restoredHTTP.Close()
	replayed := getJSON[analysisAssistantView](t, restoredHTTP.URL+"/api/v1/customers/shared/analysis/assistant")
	if len(replayed.Messages) != 2 || replayed.Messages[1].Text != got.Messages[1].Text {
		t.Fatalf("saved conversation was not restored: %+v", replayed)
	}
}

func TestAnalysisAssistantRejectsUnlinkedAnswerAndKeepsConversationEmpty(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answer := `{"answer":"A fabricated result.","sources":["F999"]}`
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer}}}})
	}))
	defer model.Close()
	server := NewServer(Config{RepoRoot: t.TempDir(), LLM: llmclient.Client{BaseURL: model.URL + "/v1", Model: "fixture"}})
	current, err := server.newRun("customer-a", "Inspect fixture", "local fixture")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	path := httpServer.URL + "/api/v1/assessments/" + current.id + "/analysis/assistant"
	if status := postStatus(t, path, analysisAssistantRequest{Question: "What did we find?"}); status != http.StatusBadGateway {
		t.Fatalf("unlinked answer status = %d", status)
	}
	got := getJSON[analysisAssistantView](t, path)
	if len(got.Messages) != 0 {
		t.Fatalf("invalid answer entered the saved conversation: %+v", got)
	}
}

func TestAnalysisAssistantSnapshotKeepsFocusWhenOtherSourcesAreOmitted(t *testing.T) {
	view := analysisView{Kind: "customer", ID: "lab", SessionCount: 1, Sessions: []analysisSessionSummary{{ID: "run-1", Goal: "Review fixture"}}}
	for i := 0; i < 80; i++ {
		view.Findings = append(view.Findings, analysisFinding{ID: fmt.Sprintf("finding-%d", i), SessionID: "run-1", Title: "Finding with long details", Impact: strings.Repeat("recorded detail ", 90)})
	}
	focus := view.Findings[len(view.Findings)-1].ID
	snapshot, refs, _, err := analysisAssistantSnapshot(view, focus, 8*1024)
	if err != nil || len(snapshot) > 8*1024 || !strings.Contains(snapshot, `"omitted_source_counts"`) || refs["F80"].ID != "F80" {
		t.Fatalf("bounded snapshot lost the selected finding: bytes=%d refs=%d err=%v", len(snapshot), len(refs), err)
	}
}

func TestAnalysisAssistantSnapshotFitsSerializedBudgetForLargeProject(t *testing.T) {
	view := analysisView{Kind: "customer", ID: strings.Repeat("project-", 13), Customer: strings.Repeat("project-", 13), SessionCount: 4}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("run-%d", i)
		view.Sessions = append(view.Sessions, analysisSessionSummary{ID: id, Goal: "Assessment", Scope: strings.Repeat("scope detail ", 35)})
		coverage := analysisCoverage{Scope: "declared target"}
		for j := 0; j < 25; j++ {
			coverage.Tests = append(coverage.Tests, analysisCoverageTest{SessionID: id, TaskID: fmt.Sprintf("task-%d", j), Summary: strings.Repeat("recorded test detail ", 100)})
		}
		view.Coverage = append(view.Coverage, coverage)
	}
	for i := 0; i < 20; i++ {
		view.Findings = append(view.Findings, analysisFinding{ID: fmt.Sprintf("finding-%d", i), Title: "Recorded observation", Impact: strings.Repeat("evidence detail ", 60)})
		view.Gaps = append(view.Gaps, strings.Repeat("open question ", 30))
	}
	for limit := 8 * 1024; limit <= 10*1024; limit += 16 {
		snapshot, refs, _, err := analysisAssistantSnapshot(view, "", limit)
		if err != nil || len(snapshot) > limit || refs["P1"].ID != "P1" {
			t.Fatalf("limit=%d snapshot bytes=%d refs=%d err=%v", limit, len(snapshot), len(refs), err)
		}
	}
}

func TestAnalysisAssistantSnapshotDoesNotRateUnconfirmedImpact(t *testing.T) {
	view := analysisView{Kind: "assessment", ID: "run-1", Findings: []analysisFinding{
		{ID: "candidate", Status: "candidate", Title: "Unconfirmed behavior", Severity: "high", Confidence: "high"},
		{ID: "verified", Status: "reproduced", Title: "Reproduced issue", Severity: "medium", Confidence: "high"},
	}}
	snapshot, _, _, err := analysisAssistantSnapshot(view, "", 16*1024)
	if err != nil {
		t.Fatal(err)
	}
	var packet struct {
		Sources []struct {
			ID     string `json:"id"`
			Detail struct {
				Severity   string `json:"severity"`
				Confidence string `json:"confidence"`
			} `json:"detail"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(snapshot), &packet); err != nil {
		t.Fatal(err)
	}
	if len(packet.Sources) < 4 || packet.Sources[2].ID != "F1" || packet.Sources[2].Detail.Severity != "" || packet.Sources[2].Detail.Confidence != "" || packet.Sources[3].ID != "F2" || packet.Sources[3].Detail.Severity != "medium" {
		t.Fatalf("assistant snapshot confused candidate and reproduced ratings: %+v", packet.Sources)
	}
}

func TestAnalysisAssistantSnapshotNamesViewScopeWithinLocalModelBudget(t *testing.T) {
	for _, kind := range []string{"assessment", "customer"} {
		view := analysisView{Kind: kind, ID: "run-1", Customer: "lab", SessionCount: 1, Summary: "Saved test summary"}
		if kind == "customer" {
			view.ID = "lab"
			view.Sessions = []analysisSessionSummary{{ID: "run-1", Goal: "Review fixture"}}
		}
		snapshot, _, _, err := analysisAssistantSnapshot(view, "", llmclient.Qwen38LabInputByteLimit/2)
		if err != nil || len(snapshot) > llmclient.Qwen38LabInputByteLimit/2 {
			t.Fatalf("%s packet exceeded local-model budget: bytes=%d err=%v", kind, len(snapshot), err)
		}
		var packet struct {
			ViewKind string `json:"view_kind"`
			ViewID   string `json:"view_id"`
		}
		if err := json.Unmarshal([]byte(snapshot), &packet); err != nil || packet.ViewKind != kind || packet.ViewID != view.ID {
			t.Fatalf("%s packet lost its view scope: %+v err=%v", kind, packet, err)
		}
	}
}

func TestAnalysisAssistantSnapshotIncludesBoundedRecordedTestCoverage(t *testing.T) {
	state := assessment.State{Scope: "192.168.50.1", Results: []assessment.Result{
		{Task: assessment.Task{ID: "tcp-survey", Goal: "Survey TCP"}, Status: "done", Summary: "All TCP ports checked. Open: TCP 53, 80, 7788, 18017, 45549."},
		{Task: assessment.Task{ID: "udp-check", Goal: "Check UDP"}, Status: "done", Summary: "UDP/53 answered; UDP/123 rejected; UDP/1900 answered."},
	}}
	coverage := coverageForAssessment("run-1", state, nil, nil)
	view := analysisView{Kind: "assessment", ID: "run-1", SessionCount: 1, Coverage: []analysisCoverage{coverage}}
	snapshot, refs, _, err := analysisAssistantSnapshot(view, "", 16*1024)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"TCP 53, 80, 7788, 18017, 45549", "UDP/53 answered", `"status":"done"`} {
		if !strings.Contains(snapshot, expected) {
			t.Fatalf("saved test coverage missing %q: %s", expected, snapshot)
		}
	}
	if refs["T1"].URL != "/analysis?assessment=run-1#analysis-details" || refs["T2"].ID != "T2" {
		t.Fatalf("recorded tests need traceable citations: %+v", refs)
	}
}
