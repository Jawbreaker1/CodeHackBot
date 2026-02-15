package cli

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestReadFileAndListDirAreBoundedToSessionOrWD(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	cfg.Session.PlanFilename = "plan.md"
	r := NewRunner(cfg, "session-fs", "", "")
	r.reader = bufio.NewReader(strings.NewReader(""))

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	wd := t.TempDir()
	origWD, _ := os.Getwd()
	_ = os.Chdir(wd)
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	// File in working dir is allowed.
	filePath := filepath.Join(wd, "demo.txt")
	if err := os.WriteFile(filePath, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Capture stdout since handlers print.
	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() { os.Stdout = orig })

	if err := r.handleReadFile([]string{"demo.txt"}); err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if err := r.handleListDir([]string{"."}); err != nil {
		t.Fatalf("list_dir: %v", err)
	}

	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	out := buf.String()
	if !strings.Contains(out, "Excerpt:") || !strings.Contains(out, "hello") {
		t.Fatalf("expected read_file output, got:\n%s", out)
	}
	if !strings.Contains(out, "Entries:") || !strings.Contains(out, "demo.txt") {
		t.Fatalf("expected list_dir output, got:\n%s", out)
	}

	// A file outside session dir and working dir should be rejected.
	other := t.TempDir()
	outsidePath := filepath.Join(other, "x.txt")
	_ = os.WriteFile(outsidePath, []byte("nope"), 0o644)
	if err := r.handleReadFile([]string{outsidePath}); err == nil {
		t.Fatalf("expected out-of-bounds error")
	}

	// Session dir is always allowed.
	planPath := filepath.Join(sessionDir, cfg.Session.PlanFilename)
	if err := r.handleReadFile([]string{planPath}); err != nil {
		t.Fatalf("read session file: %v", err)
	}
}
