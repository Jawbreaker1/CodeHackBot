package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
	"github.com/Jawbreaker1/CodeHackBot/internal/reporoot"
)

// This optional check exercises the installed browser, actual runner, approval,
// and registered artifacts against a local page. It never contacts a public site.
func TestBrowserRunReal(t *testing.T) {
	if os.Getenv("BIRDHACKBOT_LIVE_BROWSER_TEST") != "1" {
		t.Skip("set BIRDHACKBOT_LIVE_BROWSER_TEST=1 for installed-browser validation")
	}
	project, err := reporoot.Find(".")
	if err != nil {
		t.Fatal(err)
	}
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer collector.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "<html><title>Local browser fixture</title><body>Observed<img src=%q></body></html>", collector.URL+"/collect?marker=DEMO_CANARY_ONLY")
	}))
	defer server.Close()
	base, err := os.MkdirTemp(filepath.Join(project, "sessions"), "browser-tool-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	work := filepath.Join(base, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	scenario := fmt.Sprintf("export async function run({context,artifactDir,step}) { const page = await context.newPage(); await step('Visit local fixture with marker', () => page.goto(%q)); await step('Visit clean control', () => page.goto(%q)); await step('Capture local page', () => page.screenshot({path: artifactDir+'/result.png'})); }", server.URL+"/#access_token=DEMO_CANARY_ONLY", server.URL+"/?view=clean")
	if err := os.WriteFile(filepath.Join(work, "check.mjs"), []byte(scenario), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/usr/bin/chromium"); err != nil {
		t.Skip("Kali Chromium is not installed")
	}
	t.Setenv("BIRDHACKBOT_BROWSER_EXECUTABLE", "")
	response := Response{Type: "browser_run", Scenario: "check.mjs", ArtifactDir: "captures", Summary: "Visit a local fixture", Target: server.URL, Impact: "Read one local page and save a screenshot", Risk: "low", Artifacts: []string{"captures/result.png"}}
	approver := &recordingApprover{}
	loop := Loop{Executor: execx.Executor{LogDir: filepath.Join(base, "logs")}, Approver: approver}
	packet := ctxpacket.WorkerPacket{}
	packet.OperatorState.WorkingDir = work
	executed, err := loop.execute(context.Background(), &packet, response)
	if err != nil || !executed {
		t.Fatalf("real browser action: executed=%v err=%v", executed, err)
	}
	result := packet.LatestExecutionResult
	if result.ExitStatus != "0" || !strings.Contains(approver.request.Command, "run.mjs") {
		t.Fatalf("browser action did not complete through approval: %+v %+v", approver.request, result)
	}
	for _, name := range []string{"playwright-trace.zip", "browser-live.png", "browser-pages.json", "browser-network.json", "result.png"} {
		found := false
		for _, ref := range result.ArtifactRefs {
			found = found || filepath.Base(ref) == name
		}
		if !found {
			t.Fatalf("missing %s from registered artifacts: %+v", name, result.ArtifactRefs)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(work, "captures", "browser-network.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadata), "DEMO_CANARY_ONLY") {
		t.Fatal("browser network overview leaked a URL value")
	}
	var network struct {
		Requests []struct {
			CrossOrigin    bool `json:"cross_origin"`
			SourceURLShape struct {
				HasFragment bool `json:"has_fragment"`
			} `json:"source_url_shape"`
			DestinationURLShape struct {
				HasQuery bool `json:"has_query"`
			} `json:"destination_url_shape"`
		} `json:"requests"`
		BrowserSignals []struct {
			NavigatorWebdriver bool `json:"navigator_webdriver"`
		} `json:"browser_signals"`
	}
	if err := json.Unmarshal(metadata, &network); err != nil {
		t.Fatal(err)
	}
	seenBoundary := false
	seenCleanControl := false
	for _, request := range network.Requests {
		seenBoundary = seenBoundary || request.CrossOrigin && request.SourceURLShape.HasFragment && request.DestinationURLShape.HasQuery
		seenCleanControl = seenCleanControl || request.CrossOrigin && !request.SourceURLShape.HasFragment && request.DestinationURLShape.HasQuery
	}
	if !seenBoundary || !seenCleanControl || len(network.BrowserSignals) == 0 || !network.BrowserSignals[0].NavigatorWebdriver {
		t.Fatalf("missing network or automation coverage clues: %+v", network)
	}
}
