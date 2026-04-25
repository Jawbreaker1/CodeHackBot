//go:build !windows

package execx

import (
	"context"
	"testing"
)

func TestBuildCommandDetachesControllingTTY(t *testing.T) {
	cmd := buildCommand(context.Background(), Action{
		Command:  "printf hello > out.txt",
		UseShell: true,
	})
	if cmd.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr to be configured")
	}
	if !cmd.SysProcAttr.Setsid {
		t.Fatal("expected Setsid=true for non-interactive child process")
	}
}
