package cli

import (
	"io"
	"os"
	"regexp"
	"strings"

	"golang.org/x/term"
)

var ansiControlPattern = regexp.MustCompile("\x1b(?:\\[[0-9;?]*[ -/]*[@-~]|\\][^\x1b\x07]*(?:\x07|\x1b\\\\))")

func (r *Runner) liveWriter() io.Writer {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil
	}
	return &ttyWriter{out: os.Stdout}
}

type ttyWriter struct {
	out *os.File
}

func (w *ttyWriter) Write(p []byte) (int, error) {
	outputMu.Lock()
	defer outputMu.Unlock()

	if w.out == nil {
		return len(p), nil
	}

	buf := normalizeTTYBytes(p)

	_, err := w.out.Write(buf)
	return len(p), err
}

func normalizeTTYBytes(p []byte) []byte {
	if len(p) == 0 {
		return nil
	}
	raw := string(p)
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = ansiControlPattern.ReplaceAllString(normalized, "")
	normalized = strings.ReplaceAll(normalized, "\x1b", "")
	normalized = strings.ReplaceAll(normalized, "\b", "")
	// Keep explicit leading line breaks, but avoid a synthetic blank line
	// when a chunk starts with carriage-return-only progress output.
	if !strings.HasPrefix(raw, "\n") && strings.HasPrefix(normalized, "\n") {
		normalized = strings.TrimPrefix(normalized, "\n")
	}

	out := make([]byte, 0, len(normalized)*2)
	for i := 0; i < len(normalized); i++ {
		if normalized[i] == '\n' {
			out = append(out, '\r', '\n')
			continue
		}
		out = append(out, normalized[i])
	}
	return out
}
