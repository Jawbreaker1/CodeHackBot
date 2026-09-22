package webapp

import (
	"encoding/json"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWatchUsesLiveRuntimePathsAndRejectsEscapingArtifacts(t *testing.T) {
	s := NewServer(Config{RepoRoot: t.TempDir()})
	r, err := s.newRun("fixture", "watch fixture", "local only")
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(r.root, "tasks", "browser", "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(work, "run.log")
	if err := os.WriteFile(log+".stdout", []byte("Starting: click the local check\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "preview.png"), []byte("synthetic-image"), 0600); err != nil {
		t.Fatal(err)
	}
	escaped := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(escaped, []byte("outside-task"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(escaped, filepath.Join(work, "escape.png")); err != nil {
		t.Fatal(err)
	}
	r.updateWorker(assessment.Event{TaskID: "browser", Kind: "execution_started", ExecutionLog: log, ExpectedArtifacts: []string{"preview.png", "escape.png", "../../escape.png"}})
	w := httptest.NewRecorder()
	r.watch(w, httptest.NewRequest("GET", "/watch?worker=browser", nil))
	var view struct {
		Stdout string
		Images []string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Stdout, "click the local check") || len(view.Images) != 1 {
		t.Fatalf("unexpected live view: %s", w.Body.String())
	}
	for _, ref := range []string{filepath.Join(work, "preview.png"), filepath.Join(work, "escape.png")} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/artifact?path="+url.QueryEscape(ref), nil)
		s.serveAssessmentArtifact(w, req, r)
		want := 200
		if strings.HasSuffix(ref, "escape.png") {
			want = 404
		}
		if w.Code != want {
			t.Fatalf("%s: status %d", ref, w.Code)
		}
	}
	if len(r.state.Results) != 0 {
		t.Fatal("watch promoted a preview to a completed result")
	}
}
