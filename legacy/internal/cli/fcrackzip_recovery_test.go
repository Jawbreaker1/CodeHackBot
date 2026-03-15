package cli

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestFcrackzipWordlistArg(t *testing.T) {
	t.Run("separate -p arg", func(t *testing.T) {
		idx, path, ok := fcrackzipWordlistArg([]string{"-u", "-D", "-p", "/tmp/list.txt", "secret.zip"})
		if !ok {
			t.Fatalf("expected wordlist arg")
		}
		if idx != 3 || path != "/tmp/list.txt" {
			t.Fatalf("unexpected result idx=%d path=%q", idx, path)
		}
	})

	t.Run("inline -p arg", func(t *testing.T) {
		idx, path, ok := fcrackzipWordlistArg([]string{"-u", "-D", "-p/tmp/list.txt", "secret.zip"})
		if !ok {
			t.Fatalf("expected wordlist arg")
		}
		if idx != 2 || path != "/tmp/list.txt" {
			t.Fatalf("unexpected result idx=%d path=%q", idx, path)
		}
	})
}

func TestExtractGzipFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "list.txt.gz")
	dst := filepath.Join(dir, "list.txt")
	want := "alpha\nbeta\ngamma\n"

	f, err := os.Create(src)
	if err != nil {
		t.Fatalf("create gz: %v", err)
	}
	gw := gzip.NewWriter(f)
	if _, err := gw.Write([]byte(want)); err != nil {
		t.Fatalf("write gz: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	if err := extractGzipFile(src, dst); err != nil {
		t.Fatalf("extractGzipFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != want {
		t.Fatalf("unexpected extracted content: %q", string(got))
	}
}
