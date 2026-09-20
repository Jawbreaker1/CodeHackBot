package execx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCancellationStopsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	done := make(chan Result, 1)
	go func() {
		result, _ := (Executor{LogDir: dir}).Run(ctx, Action{
			Command: "sleep 5 & child=$!; printf '%s' \"$child\" > child.pid; wait", Cwd: dir, UseShell: true,
		})
		done <- result
	}()
	pid := 0
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(string(data))
		if pid > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("fixture child did not start")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	cancel()
	select {
	case result := <-done:
		if result.FailureClass != "execution_interrupted" {
			t.Fatalf("result=%+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not return promptly after cancellation")
	}
	// A reparented, terminated child may briefly remain as a zombie until reaped.
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if os.IsNotExist(err) || strings.Contains(string(status), "State:\tZ") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child is still running after worker cancellation")
}
