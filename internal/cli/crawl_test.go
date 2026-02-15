package cli

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestCrawlFetchesMultiplePagesAndWritesIndex(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>Home</title></head><body>
<a href="/a">A</a>
<a href="/b">B</a>
<a href="https://external.example/out">X</a>
</body></html>`))
	})
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>A</title></head><body><a href="/c">C</a></body></html>`))
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>B</title></head><body><a href="/c">C</a></body></html>`))
	})
	mux.HandleFunc("/c", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>C</title></head><body>ok</body></html>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Network.AssumeOffline = true
	r := NewRunner(cfg, "session-crawl", "", "")
	r.reader = bufio.NewReader(strings.NewReader("y\n"))

	if err := r.handleCrawl([]string{srv.URL, "max_pages=3", "max_depth=2", "same_host=true"}); err != nil {
		t.Fatalf("handleCrawl: %v", err)
	}

	sessionDir := filepath.Join(cfg.Session.LogDir, r.sessionID)
	indexPath := filepath.Join(sessionDir, "artifacts", "web", "crawl-index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var idx crawlIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal index: %v", err)
	}
	if idx.StartURL == "" || !strings.Contains(idx.StartURL, "http") {
		t.Fatalf("unexpected start url: %q", idx.StartURL)
	}
	if idx.SameHost != true {
		t.Fatalf("expected same_host true")
	}
	if len(idx.Pages) == 0 || len(idx.Pages) > 3 {
		t.Fatalf("unexpected pages: %d", len(idx.Pages))
	}
	for _, p := range idx.Pages {
		if p.URL == "" || p.Status == 0 {
			t.Fatalf("unexpected page entry: %+v", p)
		}
		if strings.Contains(p.URL, "external.example") {
			t.Fatalf("external link should not be crawled: %s", p.URL)
		}
		if p.BodyPath == "" {
			t.Fatalf("expected body path for %s", p.URL)
		}
	}

	pagesDir := filepath.Join(sessionDir, "artifacts", "web", "pages")
	entries, err := os.ReadDir(pagesDir)
	if err != nil {
		t.Fatalf("read pages dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected saved pages")
	}
}
