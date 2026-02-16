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

func TestWriteFileIsBoundedToSessionToolsDir(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-write", "", "")
	r.reader = bufio.NewReader(strings.NewReader(""))

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	if err := r.handleWriteFile([]string{"demo/tool.py", "print('hi')\n"}); err != nil {
		t.Fatalf("write_file: %v", err)
	}

	toolsRoot := filepath.Join(sessionDir, "artifacts", "tools")
	outPath := filepath.Join(toolsRoot, "demo", "tool.py")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if !strings.Contains(string(data), "print('hi')") {
		t.Fatalf("unexpected written content: %s", string(data))
	}

	if err := r.handleWriteFile([]string{"../escape.txt", "nope"}); err == nil {
		t.Fatalf("expected out-of-bounds error")
	}
	if err := r.handleWriteFile([]string{"/tmp/escape.txt", "nope"}); err == nil {
		t.Fatalf("expected out-of-bounds error for absolute path")
	}
}

func TestListDirTargetIgnoresFlagArgs(t *testing.T) {
	got := listDirTarget([]string{"-la"})
	if got != "." {
		t.Fatalf("expected '.', got %q", got)
	}
	got = listDirTarget([]string{"--all", "docs"})
	if got != "docs" {
		t.Fatalf("expected docs, got %q", got)
	}
}

func TestHandleListDirSupportsFileTarget(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-list-file", "", "")
	r.reader = bufio.NewReader(strings.NewReader(""))

	if _, err := r.ensureSessionScaffold(); err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}

	wd := t.TempDir()
	origWD, _ := os.Getwd()
	_ = os.Chdir(wd)
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	filePath := filepath.Join(wd, "secret.zip")
	if err := os.WriteFile(filePath, []byte("zip-bytes"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	orig := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	t.Cleanup(func() { os.Stdout = orig })

	if err := r.handleListDir([]string{"secret.zip"}); err != nil {
		t.Fatalf("list_dir on file target failed: %v", err)
	}

	_ = wOut.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	out := buf.String()
	if !strings.Contains(out, "Entry:") || !strings.Contains(out, "secret.zip") {
		t.Fatalf("expected file entry output, got:\n%s", out)
	}
}
