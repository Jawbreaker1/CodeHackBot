package reporoot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "rules")
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/test\n")
	nested := filepath.Join(root, "fixtures", "zip")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	got, err := Find(nested)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if got != root {
		t.Fatalf("Find() = %q, want %q", got, root)
	}
}

func TestFindRequiresMarkers(t *testing.T) {
	root := t.TempDir()
	if _, err := Find(root); err == nil {
		t.Fatal("Find() error = nil, want error")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
