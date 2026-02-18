//go:build windows

package orchestrator

import (
	"os/exec"
	"time"
)

func configureWorkerCommandProcess(cmd *exec.Cmd) {}

func terminateWorkerCommandProcess(cmd *exec.Cmd, _ time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
