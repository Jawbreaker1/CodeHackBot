//go:build !windows

package execx

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureNonInteractiveProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		// Give task-local runners a brief chance to persist partial results.
		// The entire process group receives both signals so children cannot keep
		// running after the worker is cancelled.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		if err != nil {
			return err
		}
		time.Sleep(750 * time.Millisecond)
		err = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}
}
