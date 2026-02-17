package cli

import (
	"fmt"
	"io"
	"os"
	"sync"
)

var outputMu sync.Mutex

type synchronizedOutputWriter struct {
	out io.Writer
}

func (w synchronizedOutputWriter) Write(p []byte) (int, error) {
	outputMu.Lock()
	defer outputMu.Unlock()
	if w.out == nil {
		return len(p), nil
	}
	return w.out.Write(p)
}

func newSynchronizedOutputWriter(out io.Writer) io.Writer {
	if out == nil {
		out = io.Discard
	}
	return synchronizedOutputWriter{out: out}
}

func safePrint(args ...any) {
	outputMu.Lock()
	defer outputMu.Unlock()
	_, _ = fmt.Fprint(os.Stdout, args...)
}

func safePrintf(format string, args ...any) {
	outputMu.Lock()
	defer outputMu.Unlock()
	_, _ = fmt.Fprintf(os.Stdout, format, args...)
}

func safePrintln(args ...any) {
	outputMu.Lock()
	defer outputMu.Unlock()
	_, _ = fmt.Fprintln(os.Stdout, args...)
}

func (r *Runner) outputWriter() io.Writer {
	return newSynchronizedOutputWriter(os.Stdout)
}
