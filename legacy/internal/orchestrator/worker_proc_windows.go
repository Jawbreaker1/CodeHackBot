//go:build windows

package orchestrator

import (
	"os/exec"
	"time"
)

func configureWorkerProcess(cmd *exec.Cmd) {}

func terminateWorkerProcess(cmd *exec.Cmd, _ time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
