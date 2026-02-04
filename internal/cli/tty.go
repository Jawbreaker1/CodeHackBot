package cli

import (
	"io"
	"os"
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
	out    *os.File
	mu     sync.Mutex
	prevCR bool
}

func (w *ttyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.out == nil {
		return len(p), nil
	}

	buf := make([]byte, 0, len(p))
	for _, b := range p {
		if b == '\n' && !w.prevCR {
			buf = append(buf, '\r')
		}
		buf = append(buf, b)
		w.prevCR = b == '\r'
	}

	_, err := w.out.Write(buf)
	return len(p), err
}
