package cli

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestBrowseSavesBodyArtifactAndLog(t *testing.T) {
	html := "<html><head><title>Demo</title><meta name=\"description\" content=\"hello\"></head><body><h1>Hi</h1>world</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Network.AssumeOffline = true
	r := NewRunner(cfg, "session-browse", "", "")
	r.reader = bufio.NewReader(strings.NewReader("y\n"))

	if err := r.handleBrowse([]string{srv.URL}); err != nil {
		t.Fatalf("handleBrowse: %v", err)
	}

	sessionDir := filepath.Join(cfg.Session.LogDir, r.sessionID)
	webDir := filepath.Join(sessionDir, "artifacts", "web")
	entries, err := os.ReadDir(webDir)
	if err != nil {
		t.Fatalf("read web artifacts: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected web body artifact file")
	}
	bodyPath := filepath.Join(webDir, entries[0].Name())
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "<title>Demo</title>") {
		t.Fatalf("unexpected body content: %s", string(body))
	}

	logDir := filepath.Join(sessionDir, "logs")
	logs, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	found := false
	for _, entry := range logs {
		if !strings.HasPrefix(entry.Name(), "web-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(logDir, entry.Name()))
		if err != nil {
			t.Fatalf("read log: %v", err)
		}
		if strings.Contains(string(data), "Body-Path:") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected browse log to include Body-Path")
	}
}

func TestNormalizeURLRejectsFlagLikeInput(t *testing.T) {
	_, err := normalizeURL("-v")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "flag") {
		t.Fatalf("expected flag-like error, got %v", err)
	}
}
