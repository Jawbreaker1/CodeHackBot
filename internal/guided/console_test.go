package guided

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(value)
}

func TestConsoleRoutesPromptAnswersAndOperatorCommands(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	var output lockedBuffer
	console := NewConsole(ctx, reader, &output)

	answer := make(chan string, 1)
	go func() {
		value, err := console.Ask(ctx, "approval")
		if err != nil {
			answer <- "error: " + err.Error()
			return
		}
		answer <- value
	}()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "approval") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(output.String(), "approval") {
		t.Fatal("prompt was not rendered")
	}
	if _, err := writer.Write([]byte("yes\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-answer:
		if got != "yes" {
			t.Fatalf("answer = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt answer was not routed")
	}
	if _, err := writer.Write([]byte("show progress\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case input := <-console.Commands():
		if input.text != "show progress" {
			t.Fatalf("command = %q", input.text)
		}
	case <-time.After(time.Second):
		t.Fatal("operator command was not routed")
	}
}
