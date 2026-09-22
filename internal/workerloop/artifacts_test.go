package workerloop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterArtifactsKeepsDeclaredFilesInsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "page.png"), []byte("fixture screenshot"), 0600); err != nil {
		t.Fatal(err)
	}
	refs, err := registerArtifacts(workspace, []string{"page.png", filepath.Join(workspace, "page.png")})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != filepath.Join(workspace, "page.png") {
		t.Fatalf("unexpected refs: %#v", refs)
	}
}

func TestRegisterArtifactsRejectsMissingOrEscapingPaths(t *testing.T) {
	workspace := t.TempDir()
	for _, declared := range []string{"missing.png", "../outside.png", filepath.Join(string(filepath.Separator), "tmp", "outside.png")} {
		_, err := registerArtifacts(workspace, []string{declared})
		if err == nil || !strings.Contains(err.Error(), "artifact") {
			t.Fatalf("declared path %q was accepted: %v", declared, err)
		}
	}
}
