package workerloop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
)

func TestBrowserRunUsesNormalApprovalAndEvidence(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is not installed")
	}
	project := t.TempDir()
	for _, name := range []string{"AGENTS.md", "go.mod"} {
		if err := os.WriteFile(filepath.Join(project, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runnerDir := filepath.Join(project, "tools", "playwright-runner")
	if err := os.MkdirAll(runnerDir, 0700); err != nil {
		t.Fatal(err)
	}
	runner := `import fs from 'node:fs'; fs.mkdirSync(process.argv[3]); for (const name of ['playwright-trace.zip','browser-live.png','browser-pages.json']) fs.writeFileSync(process.argv[3]+'/'+name, 'fixture'); console.log('Browser fixture completed');`
	if err := os.WriteFile(filepath.Join(runnerDir, "run.mjs"), []byte(runner), 0600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(project, "sessions", "task", "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "check.mjs"), []byte("export async function run() {}"), 0600); err != nil {
		t.Fatal(err)
	}
	approver := &recordingApprover{}
	loop := Loop{Executor: execx.Executor{LogDir: filepath.Join(project, "logs")}, Approver: approver}
	packet := ctxpacket.WorkerPacket{}
	packet.OperatorState.WorkingDir = work
	response, err := ParseResponse(`{"type":"browser_run","scenario":"check.mjs","artifact_dir":"browser-check-1","summary":"Inspect a local page","target":"local fixture","impact":"Open one local page","risk":"low"}`)
	if err != nil {
		t.Fatal(err)
	}
	executed, err := loop.execute(context.Background(), &packet, response)
	if err != nil || !executed {
		t.Fatalf("browser action: executed=%v err=%v", executed, err)
	}
	if approver.request.UseShell || !strings.Contains(approver.request.Command, filepath.Join(runnerDir, "run.mjs")) || approver.request.Summary != "Inspect a local page" {
		t.Fatalf("approval did not show exact browser action: %+v", approver.request)
	}
	result := packet.LatestExecutionResult
	if result.ExitStatus != "0" || len(result.LogRefs) != 1 || len(result.ArtifactRefs) != 6 {
		t.Fatalf("browser evidence not registered: %+v", result)
	}
	for _, name := range []string{"playwright-trace.zip", "browser-live.png", "browser-pages.json"} {
		found := false
		for _, ref := range result.ArtifactRefs {
			found = found || filepath.Base(ref) == name
		}
		if !found {
			t.Fatalf("missing %s from registered evidence: %+v", name, result.ArtifactRefs)
		}
	}
	if !strings.Contains(result.OutputEvidence, "Browser fixture completed") {
		t.Fatalf("browser output unavailable: %+v", result)
	}
}

func TestBrowserRunRejectsEscapingScenarioAndReusedOutput(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.mjs")
	if err := os.WriteFile(outside, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(work, "escape.mjs")); err != nil {
		t.Fatal(err)
	}
	base := Response{Type: "browser_run", Scenario: "escape.mjs", ArtifactDir: "fresh", Summary: "Inspect", Target: "fixture", Impact: "visit"}
	if _, failure := prepareBrowserRun(base, work); failure == nil {
		t.Fatal("accepted scenario symlink escaping workspace")
	}
	base.Scenario = "../outside.mjs"
	if _, failure := prepareBrowserRun(base, work); failure == nil {
		t.Fatal("accepted parent path escaping workspace")
	}
	base.ArtifactDir = "../outside"
	if _, failure := prepareBrowserRun(base, work); failure == nil {
		t.Fatal("accepted artifact directory outside workspace")
	}
}
