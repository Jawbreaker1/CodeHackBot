package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestParseLinksExtractsAndNormalizes(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-links", "", "")

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	htmlPath := filepath.Join(sessionDir, "artifacts", "web", "body.html")
	if err := os.MkdirAll(filepath.Dir(htmlPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	html := `<!doctype html>
<html>
  <head>
    <base href="https://example.com/dir/">
  </head>
  <body>
    <a href="/about#team">About</a>
    <a href="contact">Contact</a>
    <a href="javascript:alert(1)">JS</a>
    <script src="//cdn.example.com/app.js"></script>
    <img src="./img/logo.png" />
  </body>
</html>`
	if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
		t.Fatalf("write html: %v", err)
	}

	// Auto-approve any prompts (none expected here).
	r.reader = bufio.NewReader(strings.NewReader(""))

	if err := r.handleParseLinks([]string{htmlPath}); err != nil {
		t.Fatalf("handleParseLinks: %v", err)
	}

	// Find output artifact.
	webDir := filepath.Join(sessionDir, "artifacts", "web")
	entries, err := os.ReadDir(webDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var linksFile string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "links-") && strings.HasSuffix(e.Name(), ".txt") {
			linksFile = filepath.Join(webDir, e.Name())
			break
		}
	}
	if linksFile == "" {
		t.Fatalf("expected links artifact file under %s", webDir)
	}
	data, err := os.ReadFile(linksFile)
	if err != nil {
		t.Fatalf("read links: %v", err)
	}
	out := string(data)
	if strings.Contains(out, "javascript:") {
		t.Fatalf("expected javascript: links to be excluded: %s", out)
	}
	if !strings.Contains(out, "https://example.com/about") {
		t.Fatalf("expected /about normalized and fragment stripped: %s", out)
	}
	if !strings.Contains(out, "https://example.com/dir/contact") {
		t.Fatalf("expected relative link resolved against base: %s", out)
	}
	if !strings.Contains(out, "https://cdn.example.com/app.js") {
		t.Fatalf("expected protocol-relative src normalized: %s", out)
	}
	if !strings.Contains(out, "https://example.com/dir/img/logo.png") {
		t.Fatalf("expected ./img resolved against base: %s", out)
	}
}
