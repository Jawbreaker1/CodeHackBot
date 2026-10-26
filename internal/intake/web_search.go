package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WebSearchHit is a search lead. Its title and snippet are never evidence
// about a target; the selected destination page must be read separately.
type WebSearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchWeb uses the configured local SearXNG service to discover public page
// candidates. It shares URL validation with the bounded public-page reader.
func SearchWeb(ctx context.Context, baseURL, query string, client *http.Client) ([]WebSearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 200 {
		return nil, fmt.Errorf("search query must be 1–200 characters")
	}
	base, err := url.Parse(baseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("invalid local search service URL")
	}
	endpoint := *base
	endpoint.Path = strings.TrimRight(base.Path, "/") + "/search"
	params := endpoint.Query()
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("categories", "general")
	endpoint.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web search unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web search returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&result); err != nil {
		return nil, fmt.Errorf("web search returned invalid results: %w", err)
	}
	hits := make([]WebSearchHit, 0, min(len(result.Results), 8))
	seen := map[string]bool{}
	for _, entry := range result.Results {
		publicURL, err := PublicResearchURL(entry.URL)
		if err != nil || seen[publicURL] {
			continue
		}
		seen[publicURL] = true
		hits = append(hits, WebSearchHit{Title: searchExcerpt(entry.Title, 180), URL: publicURL, Snippet: searchExcerpt(entry.Content, 350)})
		if len(hits) == 8 {
			break
		}
	}
	return hits, nil
}

func searchExcerpt(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > limit {
		return value[:limit-3] + "..."
	}
	return value
}

// DiscoverLocalWebSearch only enables the optional service when its local
// health endpoint responds. Air-gapped callers must not invoke it.
func DiscoverLocalWebSearch(ctx context.Context) string {
	const localURL = "http://127.0.0.1:8888"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, localURL+"/healthz", nil)
	if err != nil {
		return ""
	}
	client := &http.Client{Timeout: 750 * time.Millisecond}
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK {
		return localURL
	}
	return ""
}
