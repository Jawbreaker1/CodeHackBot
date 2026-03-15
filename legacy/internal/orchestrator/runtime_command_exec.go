package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

func runWorkerCommand(ctx context.Context, cmd *exec.Cmd, grace time.Duration) ([]byte, error) {
	if cmd == nil {
		return nil, fmt.Errorf("nil command")
	}
	configureWorkerCommandProcess(cmd)
	output := newCappedOutputBuffer(workerCommandOutputMaxBytes)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return output.Bytes(), err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return output.Bytes(), err
	case <-ctx.Done():
		terminateWorkerCommandProcess(cmd, grace)
		select {
		case <-done:
		case <-time.After(grace):
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return output.Bytes(), errWorkerCommandTimeout
		}
		return output.Bytes(), errWorkerCommandInterrupted
	}
}

type cappedOutputBuffer struct {
	data      []byte
	max       int
	truncated bool
}

func newCappedOutputBuffer(max int) *cappedOutputBuffer {
	if max <= 0 {
		max = workerCommandOutputMaxBytes
	}
	return &cappedOutputBuffer{
		data: make([]byte, 0, min(4096, max)),
		max:  max,
	}
}

func (c *cappedOutputBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(c.data) < c.max {
		remaining := c.max - len(c.data)
		if len(p) <= remaining {
			c.data = append(c.data, p...)
		} else {
			c.data = append(c.data, p[:remaining]...)
			c.truncated = true
		}
	} else {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedOutputBuffer) Bytes() []byte {
	out := append([]byte(nil), c.data...)
	if !c.truncated {
		return out
	}
	remaining := c.max - len(out)
	if remaining <= 0 {
		return out
	}
	note := []byte(workerCommandTruncationNote)
	if len(note) > remaining {
		note = note[:remaining]
	}
	return append(out, note...)
}
