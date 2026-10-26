package intake

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type searchRoundTrip func(*http.Request) (*http.Response, error)

func (f searchRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNVDSearchReturnsRecentCitableResults(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: searchRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "services.nvd.nist.gov" || r.URL.Query().Get("keywordSearch") != "PHP core" {
			t.Fatalf("unexpected search target: %s", r.URL)
		}
		body := `{"totalResults":3,"vulnerabilities":[{"cve":{"id":"CVE-2026-0001","published":"2026-01-01","descriptions":[{"lang":"en","value":"old result"}]}}]}`
		if requests == 2 {
			if r.URL.Query().Get("startIndex") != "0" || r.URL.Query().Get("resultsPerPage") != "3" {
				t.Fatalf("unexpected result window: %s", r.URL)
			}
			body = `{"totalResults":3,"vulnerabilities":[{"cve":{"id":"CVE-2026-0001","published":"2026-01-01","descriptions":[{"lang":"en","value":"old result"}]}},{"cve":{"id":"CVE-2026-0002","published":"2026-02-01","descriptions":[{"lang":"en","value":"middle result"}]}},{"cve":{"id":"CVE-2026-0003","published":"2026-03-01","descriptions":[{"lang":"en","value":"new result"}]}}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	results, err := searchPublicVulnerabilities(context.Background(), "PHP core", client)
	if err != nil || requests != 2 || len(results) != 3 || results[0].ID != "CVE-2026-0003" || results[0].URL != "https://nvd.nist.gov/vuln/detail/CVE-2026-0003" {
		t.Fatalf("results=%+v requests=%d err=%v", results, requests, err)
	}
}

func TestNVDSearchRejectsPrivateOrMalformedQueries(t *testing.T) {
	for _, query := range []string{"https://internal.example/path", "user@example.com", "x", "a\nb"} {
		if _, err := searchPublicVulnerabilities(context.Background(), query, &http.Client{}); err == nil {
			t.Fatalf("accepted query %q", query)
		}
	}
}
