package webapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

func TestAssessmentArtifactsShowsSavedFilesWithoutExecutionLogs(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "tasks", "browser", "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	screenshot := filepath.Join(work, "page.png")
	result := filepath.Join(work, "result.txt")
	log := filepath.Join(work, "run.log")
	for _, path := range []string{screenshot, result, log} {
		if err := os.WriteFile(path, []byte("saved evidence"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := assessment.State{Results: []assessment.Result{{
		Task:     assessment.Task{ID: "browser"},
		Evidence: []ctxpacket.ExecutionResult{{ArtifactRefs: []string{screenshot, result, log, filepath.Join(work, "missing.txt")}, LogRefs: []string{log}}},
	}}}
	workers := map[string]workerView{"browser": {ID: "browser", Evidence: []assessment.EvidenceView{{ArtifactRefs: []string{screenshot}}}}}
	artifacts := assessmentArtifacts(root, "session", state, workers)
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %+v; want one image and one file without logs or duplicates", artifacts)
	}
	if artifacts[0].Kind != "image" || artifacts[0].Name != "page.png" || artifacts[0].TaskID != "browser" {
		t.Fatalf("image artifact = %+v", artifacts[0])
	}
	if artifacts[1].Kind != "file" || artifacts[1].Name != "result.txt" {
		t.Fatalf("file artifact = %+v", artifacts[1])
	}
	for _, artifact := range artifacts {
		if !strings.Contains(artifact.URL, "/api/v1/assessments/session/artifact?path=") {
			t.Fatalf("unexpected artifact URL: %q", artifact.URL)
		}
	}
}
