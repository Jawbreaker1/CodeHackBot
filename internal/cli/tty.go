package cli

import (
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

func (r *Runner) liveWriter() io.Writer {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil
	}
	return &ttyWriter{out: os.Stdout}
}

type ttyWriter struct {
	out *os.File
	mu  sync.Mutex
}

func (w *ttyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

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
	normalized := strings.ReplaceAll(string(p), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

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
