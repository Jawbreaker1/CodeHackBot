package webapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
	"github.com/Jawbreaker1/CodeHackBot/internal/reporoot"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

func TestLivePublicPageResearch(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_LIVE_ANALYSIS_FETCH") != "1" {
		t.Skip("set BIRDHACKBOT_LIVE_ANALYSIS_FETCH=1 to test the shared public web fetch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	sources, err := (publicPageResearcher{}).Read(ctx, []string{"https://www.php.net/"})
	if err != nil || len(sources) == 0 || sources[0].URL == "" || sources[0].Excerpt == "" {
		t.Fatalf("default public research sources=%+v err=%v", sources, err)
	}
	t.Logf("read %d current public sources, first=%s", len(sources), sources[0].URL)
}

// Opt-in provider smoke: BIRDHACKBOT_LIVE_ANALYSIS_RESEARCH=1 go test
// ./internal/webapp -run TestLiveAnalysisResearch -v -count=1
func TestLiveAnalysisResearch(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_LIVE_ANALYSIS_RESEARCH") != "1" {
		t.Skip("live provider smoke is opt-in")
	}
	root, err := reporoot.Find(".")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadModelProfiles(filepath.Join(root, "config", "model-profiles.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var daybreak *ModelProfile
	for i := range profiles.Profiles {
		if profiles.Profiles[i].ID == "daybreak" {
			daybreak = &profiles.Profiles[i]
			break
		}
	}
	if daybreak == nil || !daybreak.ManagedBridge {
		t.Skip("managed Daybreak profile is not configured")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	bridge, err := subscription.StartLocalBridge(ctx, filepath.Join(home, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	daybreak.BaseURL, daybreak.TokenFile = bridge.BaseURL, bridge.TokenFile
	researcher := publicPageResearcher{
		search: func(_ context.Context, query string) ([]intake.VulnerabilityResult, error) {
			t.Logf("model-selected public search: %s", query)
			return []intake.VulnerabilityResult{{ID: "CVE-2024-4577", URL: "https://nvd.nist.gov/vuln/detail/CVE-2024-4577", Description: "Synthetic test source: a PHP CGI issue is relevant only under specific configurations.", Retrieved: time.Now().UTC()}}, nil
		},
		fetch: func(_ context.Context, raw string) (intake.ResearchPage, error) {
			t.Logf("model-selected public URL: %s", raw)
			return intake.ResearchPage{URL: raw, Status: 200, Excerpt: "Synthetic test source: an affected PHP component may expose a session token when a specific feature is enabled.", ObservedAt: time.Now().UTC()}, nil
		},
	}
	var activeResearcher analysisResearcher = researcher
	if os.Getenv("BIRDHACKBOT_LIVE_ANALYSIS_REAL_FETCH") == "1" {
		activeResearcher = publicPageResearcher{
			search: func(ctx context.Context, query string) ([]intake.VulnerabilityResult, error) {
				results, err := intake.SearchPublicVulnerabilities(ctx, query)
				t.Logf("public search %q: %d results; error=%v", query, len(results), err)
				return results, err
			},
			fetch: func(ctx context.Context, raw string) (intake.ResearchPage, error) {
				page, err := intake.FetchPublicResearchPage(ctx, raw)
				t.Logf("public page %s: status=%d; error=%v", raw, page.Status, err)
				return page, err
			},
		}
	}
	previous := []analysisAssistantMessage{
		{Role: "user", Text: "Why are people worried about PHP issues lately?"},
		{Role: "assistant", Text: "I cannot perform web research from this saved-analysis view."},
	}
	answer, _, err := askAnalysisAssistantWithResearch(ctx, daybreak.client(), analysisView{Kind: "assessment", ID: "fixture-run", SessionCount: 1, Summary: "This fixture has no PHP inventory or security test."}, previous,
		analysisAssistantRequest{Question: "Why are people concerned about recent PHP security issues? I want you to do web research. Does this assessment establish that the target is affected?"}, activeResearcher)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("analysis answer: %s; sources=%+v", answer.Text, answer.Sources)
	if len(answer.Sources) == 0 || answer.Sources[0].Kind != "web" || !strings.Contains(strings.ToLower(answer.Text), "php") {
		t.Fatalf("live model did not use labeled research source: %+v", answer)
	}
}

// Opt-in provider smoke for the model's broad-search decision and its cited
// answer. Search and fetched pages are controlled fixtures; no search engine
// or customer target is contacted by this test.
func TestLiveAnalysisBroadSearch(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_LIVE_ANALYSIS_BROAD_SEARCH") != "1" {
		t.Skip("live broad-search provider smoke is opt-in")
	}
	root, err := reporoot.Find(".")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadModelProfiles(filepath.Join(root, "config", "model-profiles.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var daybreak *ModelProfile
	for i := range profiles.Profiles {
		if profiles.Profiles[i].ID == "daybreak" {
			daybreak = &profiles.Profiles[i]
			break
		}
	}
	if daybreak == nil || !daybreak.ManagedBridge {
		t.Skip("managed Daybreak profile is not configured")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	bridge, err := subscription.StartLocalBridge(ctx, filepath.Join(home, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	daybreak.BaseURL, daybreak.TokenFile = bridge.BaseURL, bridge.TokenFile
	queries, reads := 0, 0
	researcher := publicPageResearcher{
		webSearch: fakeAnalysisWebSearcher(func(_ context.Context, query string) ([]analysisWebHit, error) {
			queries++
			t.Logf("model-selected broad query: %s", query)
			return []analysisWebHit{{Title: "PHP security releases", URL: "https://www.php.net/", Snippet: "Public release information"}}, nil
		}),
		fetch: func(_ context.Context, raw string) (intake.ResearchPage, error) {
			reads++
			return intake.ResearchPage{URL: raw, Status: 200, Excerpt: "This is a controlled fixture. PHP publishes security updates; the saved assessment contains no PHP inventory or version evidence.", ObservedAt: time.Now().UTC()}, nil
		},
	}
	realSearch := os.Getenv("BIRDHACKBOT_LIVE_ANALYSIS_REAL_SEARCH") == "1"
	if realSearch {
		researcher = publicPageResearcher{webSearch: searxNGSearch{baseURL: "http://127.0.0.1:8888"}}
	}
	answer, _, err := askAnalysisAssistantWithResearch(ctx, daybreak.client(), analysisView{Kind: "assessment", ID: "fixture-run", SessionCount: 1, Summary: "No PHP inventory was recorded."}, nil,
		analysisAssistantRequest{Question: "Why are people worried about PHP security issues lately? Search the web for current context, then explain whether this assessment proves the target is affected."}, researcher)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("answer: %s; sources=%+v", answer.Text, answer.Sources)
	if realSearch && len(answer.Sources) > 0 && answer.Sources[0].Kind == "web" {
		return
	}
	if queries != 1 || reads != 1 || len(answer.Sources) == 0 || answer.Sources[0].Kind != "web" {
		t.Fatalf("broad research was not used: queries=%d reads=%d answer=%+v", queries, reads, answer)
	}
}
