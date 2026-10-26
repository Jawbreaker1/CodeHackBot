package webapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
)

func TestAnalysisResearchReusesBoundedPublicPageFetch(t *testing.T) {
	reads := 0
	researcher := publicPageResearcher{fetch: func(_ context.Context, raw string) (intake.ResearchPage, error) {
		reads++
		if raw != "https://www.php.net/" {
			t.Errorf("unexpected public page %q", raw)
		}
		return intake.ResearchPage{URL: raw, Status: 200, Excerpt: "Current vendor advisory", ObservedAt: time.Now().UTC()}, nil
	}}
	sources, err := researcher.Read(context.Background(), []string{"http://127.0.0.1/private", "https://www.php.net/"})
	if err != nil || reads != 1 || len(sources) != 1 || sources[0].ID != "W1" || sources[0].Excerpt != "Current vendor advisory" {
		t.Fatalf("sources=%+v reads=%d err=%v", sources, reads, err)
	}
	_, err = researcher.Read(context.Background(), []string{"https://www.php.net/?token=secret"})
	if err == nil || !strings.Contains(err.Error(), "no selected public page") {
		t.Fatalf("query-bearing URL should not be fetched: %v", err)
	}
	if reads != 1 {
		t.Fatalf("unexpected fetch count: %d", reads)
	}
}

func TestConnectedAnalysisResearchNeedsNoProvider(t *testing.T) {
	server := NewServer(Config{RepoRoot: t.TempDir()})
	if _, ok := server.analysisResearcher.(publicPageResearcher); !ok {
		t.Fatalf("expected shared public-page fetch, got %T", server.analysisResearcher)
	}
}

func TestAnalysisWebSearchAvailabilityRespectsOfflineMode(t *testing.T) {
	connected := NewServer(Config{RepoRoot: t.TempDir(), WebSearchURL: "http://127.0.0.1:8888"})
	if !connected.analysisAssistantView(analysisAssistantRecord{}, false).WebSearchAvailable {
		t.Fatal("configured connected search should be shown")
	}
	offline := NewServer(Config{RepoRoot: t.TempDir(), WebSearchURL: "http://127.0.0.1:8888", Frame: behavior.Frame{Parameters: map[string]string{"research_mode": "air_gapped"}}})
	if offline.analysisAssistantView(analysisAssistantRecord{}, false).WebSearchAvailable {
		t.Fatal("air-gapped mode must hide web search")
	}
}
