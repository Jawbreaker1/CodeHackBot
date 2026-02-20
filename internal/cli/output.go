package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

var outputMu sync.Mutex

func withOutputLock(fn func()) {
	outputMu.Lock()
	defer outputMu.Unlock()
	fn()
}

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
	var buf bytes.Buffer
	_, _ = fmt.Fprint(&buf, args...)
	writeConsoleLocked(buf.String())
}

func safePrintf(format string, args ...any) {
	outputMu.Lock()
	defer outputMu.Unlock()
	writeConsoleLocked(fmt.Sprintf(format, args...))
}

func safePrintln(args ...any) {
	outputMu.Lock()
	defer outputMu.Unlock()
	var buf bytes.Buffer
	_, _ = fmt.Fprintln(&buf, args...)
	writeConsoleLocked(buf.String())
}

func (r *Runner) outputWriter() io.Writer {
	return newSynchronizedOutputWriter(os.Stdout)
}

func writeConsoleLocked(s string) {
	if s == "" {
		return
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		s = normalizeConsoleNewlines(s)
	}
	_, _ = io.WriteString(os.Stdout, s)
}

func normalizeConsoleNewlines(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}
