package webapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/intake"
)

// Analysis research uses the same bounded public-page fetch as coordinator
// observation. It is background reading, never evidence about a target.
type analysisResearcher interface {
	Read(context.Context, []string) ([]analysisResearchSource, error)
}

type analysisSearcher interface {
	Search(context.Context, string) ([]analysisResearchSource, error)
}

// A broad search provider is optional. Only its candidates are sent to the
// model; a selected page must still pass the bounded public-page reader.
type analysisWebSearcher interface {
	SearchWeb(context.Context, string) ([]analysisWebHit, error)
}

type analysisWebHit = intake.WebSearchHit

type analysisResearchSource struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Status    int       `json:"status"`
	Excerpt   string    `json:"excerpt"`
	Retrieved time.Time `json:"retrieved_at"`
}

type publicPageResearcher struct {
	fetch     func(context.Context, string) (intake.ResearchPage, error)
	search    func(context.Context, string) ([]intake.VulnerabilityResult, error)
	webSearch analysisWebSearcher
}

func (r publicPageResearcher) SearchWeb(ctx context.Context, query string) ([]analysisWebHit, error) {
	if r.webSearch == nil {
		return nil, fmt.Errorf("broad web search is not configured")
	}
	return r.webSearch.SearchWeb(ctx, query)
}

func (r publicPageResearcher) HasWebSearch() bool { return r.webSearch != nil }

func (r publicPageResearcher) Search(ctx context.Context, query string) ([]analysisResearchSource, error) {
	search := r.search
	if search == nil {
		search = intake.SearchPublicVulnerabilities
	}
	hits, err := search(ctx, query)
	if err != nil {
		return nil, err
	}
	sources := make([]analysisResearchSource, 0, len(hits))
	for _, hit := range hits {
		if hit.ID == "" || hit.URL == "" || hit.Description == "" {
			continue
		}
		sources = append(sources, analysisResearchSource{ID: fmt.Sprintf("W%d", len(sources)+1), Title: hit.ID + " · NVD", URL: hit.URL, Status: 200, Excerpt: hit.Published + " — " + hit.Description, Retrieved: hit.Retrieved})
	}
	return sources, nil
}

func (r publicPageResearcher) Read(ctx context.Context, candidates []string) ([]analysisResearchSource, error) {
	if len(candidates) == 0 || len(candidates) > 2 {
		return nil, fmt.Errorf("choose one or two public research pages")
	}
	fetch := r.fetch
	if fetch == nil {
		fetch = intake.FetchPublicResearchPage
	}
	sources := make([]analysisResearchSource, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		publicURL, err := intake.PublicResearchURL(candidate)
		if err != nil || seen[publicURL] {
			continue
		}
		seen[publicURL] = true
		page, err := fetch(ctx, publicURL)
		if err != nil || page.Status < 200 || page.Status >= 300 || strings.TrimSpace(page.Excerpt) == "" {
			continue
		}
		excerpt := page.Excerpt
		if len(excerpt) > 20<<10 {
			excerpt = excerpt[:20<<10]
		}
		sources = append(sources, analysisResearchSource{ID: fmt.Sprintf("W%d", len(sources)+1), Title: publicURL, URL: publicURL, Status: page.Status, Excerpt: excerpt, Retrieved: page.ObservedAt})
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no selected public page was readable")
	}
	return sources, nil
}
