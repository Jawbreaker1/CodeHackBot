//go:build !windows

package execx

import (
	"os/exec"
	"syscall"
)

func configureNonInteractiveProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
