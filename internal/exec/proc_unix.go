//go:build !windows

package exec

import (
	"os/exec"
	"syscall"
)

func configureCommandProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateCommandProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid <= 0 {
		return
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid > 0 {
		// Negative PGID targets the full process group (shell + spawned children).
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return
	}
	_ = cmd.Process.Kill()
}
