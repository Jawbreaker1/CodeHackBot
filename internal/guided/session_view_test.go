package guided

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestSessionViewsShowCurrentPlanAndFindings(t *testing.T) {
	var output bytes.Buffer
	console := newConsole(context.Background(), strings.NewReader(""), &output, nil)
	state := assessment.State{
		Plans: []assessment.Decision{
			{Summary: "old plan", Findings: []assessment.Finding{{Title: "obsolete lead"}}},
			{Summary: "Check the access boundary", Review: "The earlier check did not reproduce the issue.", Tasks: []assessment.Task{{ID: "verify", Goal: "Check both tenants", DoneWhen: "both responses recorded"}}, Findings: []assessment.Finding{{Title: "Access issue", Status: "candidate", Severity: "high", Impact: "A reader may see another tenant's data."}}},
		},
	}
	if !printSessionView(console, "/plan", t.TempDir(), state) || !printSessionView(console, "/findings", t.TempDir(), state) {
		t.Fatal("known session command was not handled")
	}
	got := output.String()
	for _, value := range []string{"Plan 2", "Previous round:", "verify [proposed]", "Access issue", "A reader may see another tenant's data."} {
		if !strings.Contains(got, value) {
			t.Fatalf("missing %q in %q", value, got)
		}
	}
	if strings.Contains(got, "obsolete lead") {
		t.Fatal("superseded finding appeared as current")
	}
}

func TestAutomaticApprovalStillShowsCoordinatorPlan(t *testing.T) {
	var output bytes.Buffer
	console := newConsole(context.Background(), strings.NewReader(""), &output, nil)
	console.permissionMode = approval.FullAccess
	plan := assessment.Decision{Summary: "Inspect the public surface", PlainSummary: "Establish what is exposed.", Tasks: []assessment.Task{{ID: "inspect", Goal: "Check responses", DoneWhen: "responses recorded"}}}
	review, err := reviewCoordinatorPlan(context.Background(), console, plan)
	if err != nil || len(review.TaskIDs) != 1 || review.TaskIDs[0] != "inspect" {
		t.Fatalf("automatic plan review = %#v, %v", review, err)
	}
	for _, value := range []string{"Coordinator plan:", "Purpose: Establish what is exposed.", "inspect", "Starting these tasks"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("missing %q in %q", value, output.String())
		}
	}
}

func TestSessionArtifactsShowOnlyExistingRegisteredWorkerFiles(t *testing.T) {
	root := t.TempDir()
	artifact := filepath.Join(root, "tasks", "inspect", "work", "capture.png")
	if err := os.MkdirAll(filepath.Dir(artifact), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "tasks", "inspect", "logs", "run.stdout")
	if err := os.MkdirAll(filepath.Dir(log), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, []byte("output"), 0600); err != nil {
		t.Fatal(err)
	}
	state := assessment.State{Results: []assessment.Result{{Task: assessment.Task{ID: "inspect"}, Evidence: []ctxpacket.ExecutionResult{{ArtifactRefs: []string{artifact, artifact, log, filepath.Join(root, "missing.png"), "/outside/file.png"}}}}}}
	refs := savedArtifacts(root, state)
	if len(refs) != 1 || !strings.Contains(refs[0], "capture.png") {
		t.Fatalf("artifacts = %#v", refs)
	}
}
