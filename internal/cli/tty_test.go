package cli

import "testing"

func TestNormalizeTTYBytes(t *testing.T) {
	in := []byte("line1\rline2\r\nline3\n")
	got := string(normalizeTTYBytes(in))
	want := "line1\r\nline2\r\nline3\r\n"
	if got != want {
		t.Fatalf("normalizeTTYBytes mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestNormalizeTTYBytesStripsANSIControlSequences(t *testing.T) {
	in := []byte("\x1b[2K\rscan progress\x1b[31m red\x1b[0m\nnext\bline\n")
	got := string(normalizeTTYBytes(in))
	want := "scan progress red\r\nnextline\r\n"
	if got != want {
		t.Fatalf("normalizeTTYBytes mismatch:\n got: %q\nwant: %q", got, want)
	}
}
