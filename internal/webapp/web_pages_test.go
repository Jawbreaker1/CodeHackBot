package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestObservedBrowserPagesAndNotes(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "tasks", "browser", "work")
	artifacts := filepath.Join(work, "browser-artifacts")
	if err := os.MkdirAll(artifacts, 0700); err != nil {
		t.Fatal(err)
	}
	anchor := filepath.Join(artifacts, "browser-live.png")
	shot := filepath.Join(artifacts, "browser-page-002.png")
	for _, path := range []string{anchor, shot} {
		if err := os.WriteFile(path, []byte("image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := browserManifest{Version: 1}
	manifest.Pages = append(manifest.Pages,
		struct {
			URL        string `json:"url"`
			Screenshot string `json:"screenshot"`
		}{URL: "https://example.test/", Screenshot: ""},
		struct {
			URL        string `json:"url"`
			Screenshot string `json:"screenshot"`
		}{URL: "https://example.test/login?token=secret", Screenshot: "browser-page-002.png"},
	)
	manifest.Transitions = append(manifest.Transitions, struct {
		From string `json:"from"`
		To   string `json:"to"`
	}{From: "https://example.test/", To: "https://example.test/login"})
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "browser-pages.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	state := assessment.State{ID: "assessment", Status: "completed", Results: []assessment.Result{{Task: assessment.Task{ID: "browser"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{ArtifactRefs: []string{anchor}}}}}}
	current := &run{id: "assessment", root: root, state: state}
	view := current.analysis()
	if len(view.WebPages) != 2 || len(view.WebTransitions) != 1 || view.WebPages[1].URL != "https://example.test/login" || view.WebPages[1].ScreenshotPath != shot {
		t.Fatalf("unexpected browser analysis: pages=%+v transitions=%+v", view.WebPages, view.WebTransitions)
	}
	if err := current.saveWebNote(webNote{TaskID: "browser", URL: "https://example.test/login", Comment: "Check the login response."}); err != nil {
		t.Fatal(err)
	}
	if got := current.analysis().WebPages[1].Comment; got != "Check the login response." {
		t.Fatalf("saved note=%q", got)
	}
	if err := current.saveWebNote(webNote{TaskID: "browser", URL: "https://example.test/unvisited", Comment: "Claim"}); err == nil {
		t.Fatal("accepted note for unvisited page")
	}
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, artifactURL(current.id, shot), nil)
	recorder := httptest.NewRecorder()
	server.serveAssessmentArtifact(recorder, request, current)
	if recorder.Code != http.StatusOK {
		t.Fatalf("screenshot status=%d", recorder.Code)
	}
	unrelated := filepath.Join(artifacts, "unrelated.png")
	if err := os.WriteFile(unrelated, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, artifactURL(current.id, unrelated), nil)
	recorder = httptest.NewRecorder()
	server.serveAssessmentArtifact(recorder, request, current)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unregistered screenshot status=%d", recorder.Code)
	}
}

func TestLegacyBrowserPreviewAppearsAsOneObservedPage(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "tasks", "browser", "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	anchor := filepath.Join(work, "browser-live.png")
	if err := os.WriteFile(anchor, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "browser-live.json"), []byte(`{"url":"https://example.test/page?secret=value","phase":"finished"}`), 0600); err != nil {
		t.Fatal(err)
	}
	pages, transitions := browserAnalysis(root, "assessment", []browserAnchor{{TaskID: "browser", Path: anchor}})
	if len(pages) != 1 || pages[0].URL != "https://example.test/page" || len(transitions) != 0 {
		t.Fatalf("pages=%+v transitions=%+v", pages, transitions)
	}
}
