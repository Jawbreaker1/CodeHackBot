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

// VulnerabilityResult is a search hit, not evidence that any assessed target is affected.
type VulnerabilityResult struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Published   string    `json:"published,omitempty"`
	Retrieved   time.Time `json:"retrieved_at"`
}

// SearchPublicVulnerabilities searches NVD's public CVE index. The query is
// inserted into a fixed API endpoint; it can never select a network host.
func SearchPublicVulnerabilities(ctx context.Context, query string) ([]VulnerabilityResult, error) {
	client := &http.Client{
		Timeout:       22 * time.Second,
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	return searchPublicVulnerabilities(ctx, query, client)
}

func searchPublicVulnerabilities(ctx context.Context, query string, client *http.Client) ([]VulnerabilityResult, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 || len(query) > 100 || strings.ContainsAny(query, "\r\n/@") || strings.Contains(query, "://") {
		return nil, fmt.Errorf("search for a short public product, component, or CVE name")
	}
	ctx, cancel := context.WithTimeout(ctx, 22*time.Second)
	defer cancel()
	const endpoint = "https://services.nvd.nist.gov/rest/json/cves/2.0"
	params := url.Values{"resultsPerPage": {"1"}}
	if strings.HasPrefix(strings.ToUpper(query), "CVE-") && !strings.Contains(query, " ") {
		params.Set("cveId", strings.ToUpper(query))
	} else {
		params.Set("keywordSearch", query)
	}
	lookup := func() (nvdResponse, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
		if err != nil {
			return nvdResponse{}, err
		}
		request.Header.Set("User-Agent", "BirdHackBot/1.0 (public vulnerability research)")
		response, err := client.Do(request)
		if err != nil {
			return nvdResponse{}, fmt.Errorf("NVD search: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nvdResponse{}, fmt.Errorf("NVD search returned HTTP %d", response.StatusCode)
		}
		var result nvdResponse
		if err := json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&result); err != nil {
			return nvdResponse{}, fmt.Errorf("read NVD results: %w", err)
		}
		return result, nil
	}
	first, err := lookup()
	if err != nil {
		return nil, err
	}
	if first.TotalResults == 0 {
		return []VulnerabilityResult{}, nil
	}
	const limit = 8
	if first.TotalResults > 1 {
		count := min(first.TotalResults, limit)
		params.Set("resultsPerPage", fmt.Sprint(count))
		params.Set("startIndex", fmt.Sprint(first.TotalResults-count))
		first, err = lookup()
		if err != nil {
			return nil, err
		}
	}
	results := make([]VulnerabilityResult, 0, len(first.Vulnerabilities))
	now := time.Now().UTC()
	for n := len(first.Vulnerabilities) - 1; n >= 0; n-- {
		item := first.Vulnerabilities[n].CVE
		if !strings.HasPrefix(item.ID, "CVE-") {
			continue
		}
		description := ""
		for _, text := range item.Descriptions {
			if text.Lang == "en" {
				description = text.Value
				break
			}
		}
		if len(description) > 1800 {
			description = description[:1800]
		}
		results = append(results, VulnerabilityResult{ID: item.ID, URL: "https://nvd.nist.gov/vuln/detail/" + item.ID, Description: description, Published: item.Published, Retrieved: now})
	}
	return results, nil
}

type nvdResponse struct {
	TotalResults    int `json:"totalResults"`
	Vulnerabilities []struct {
		CVE struct {
			ID           string `json:"id"`
			Published    string `json:"published"`
			Descriptions []struct {
				Lang  string `json:"lang"`
				Value string `json:"value"`
			} `json:"descriptions"`
		} `json:"cve"`
	} `json:"vulnerabilities"`
}
