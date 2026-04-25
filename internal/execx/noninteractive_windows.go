//go:build windows

package execx

import "os/exec"

func configureNonInteractiveProcess(cmd *exec.Cmd) {}
