package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllocateSessionPathsUsesExplicitSessionDir(t *testing.T) {
	repoRoot := t.TempDir()
	explicit := filepath.Join(repoRoot, "custom-session")
	paths, err := allocateSessionPaths(repoRoot, "", explicit, "run")
	if err != nil {
		t.Fatalf("allocateSessionPaths() error = %v", err)
	}
	if paths.Root != explicit {
		t.Fatalf("Root = %q, want %q", paths.Root, explicit)
	}
	if paths.LogsDir != filepath.Join(explicit, "logs") {
		t.Fatalf("LogsDir = %q", paths.LogsDir)
	}
	if paths.ContextDir != filepath.Join(explicit, "context") {
		t.Fatalf("ContextDir = %q", paths.ContextDir)
	}
	if paths.StatePath != filepath.Join(explicit, "session.json") {
		t.Fatalf("StatePath = %q", paths.StatePath)
	}
}

func TestAllocateSessionPathsCreatesFreshRunUnderSessionsRoot(t *testing.T) {
	repoRoot := t.TempDir()
	sessionsRoot := filepath.Join(repoRoot, "runtime-sessions")
	paths, err := allocateSessionPaths(repoRoot, sessionsRoot, "", "router")
	if err != nil {
		t.Fatalf("allocateSessionPaths() error = %v", err)
	}
	if !strings.HasPrefix(paths.Root, sessionsRoot+string(filepath.Separator)+"router-") {
		t.Fatalf("Root = %q, want prefix %q", paths.Root, sessionsRoot+string(filepath.Separator)+"router-")
	}
	for _, want := range []string{paths.Root, paths.LogsDir, paths.ContextDir} {
		if infoErr := assertDirExists(want); infoErr != nil {
			t.Fatalf("expected dir %q: %v", want, infoErr)
		}
	}
}

func TestExistingSessionPathsNormalizesRoot(t *testing.T) {
	root := t.TempDir()
	paths, err := existingSessionPaths(root)
	if err != nil {
		t.Fatalf("existingSessionPaths() error = %v", err)
	}
	if paths.Root != root {
		t.Fatalf("Root = %q, want %q", paths.Root, root)
	}
	if paths.StatePath != filepath.Join(root, "session.json") {
		t.Fatalf("StatePath = %q", paths.StatePath)
	}
}

func TestIsAbortedUsesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !isAborted(ctx, errors.New("boom")) {
		t.Fatal("expected aborted")
	}
	if got := terminalError(ctx, errors.New("boom")); got != "aborted by signal" {
		t.Fatalf("terminalError() = %q", got)
	}
}

func TestIsAbortedFalseForNormalError(t *testing.T) {
	ctx := context.Background()
	if isAborted(ctx, nil) {
		t.Fatal("expected not aborted for nil error")
	}
	if isAborted(ctx, errors.New("boom")) {
		t.Fatal("expected not aborted for normal error")
	}
	if got := terminalError(ctx, errors.New("boom")); got != "boom" {
		t.Fatalf("terminalError() = %q", got)
	}
}

func assertDirExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return filepath.ErrBadPattern
	}
	return nil
}
