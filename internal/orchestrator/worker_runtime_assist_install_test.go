package orchestrator

import (
	"errors"
	"os/exec"
	"testing"
)

func TestParseToolInstallPolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    ToolInstallPolicy
		wantErr bool
	}{
		{in: "", want: ToolInstallPolicyAsk},
		{in: "ask", want: ToolInstallPolicyAsk},
		{in: "never", want: ToolInstallPolicyNever},
		{in: "auto", want: ToolInstallPolicyAuto},
		{in: "invalid", wantErr: true},
	}
	for _, tc := range cases {
		got, err := ParseToolInstallPolicy(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseToolInstallPolicy(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseToolInstallPolicy(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseToolInstallPolicy(%q): got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestDetectMissingExecutableFromExecError(t *testing.T) {
	t.Parallel()

	runErr := &exec.Error{Name: "nuclei", Err: exec.ErrNotFound}
	got := detectMissingExecutable("nuclei", runErr, nil)
	if got != "nuclei" {
		t.Fatalf("detectMissingExecutable exec.Error: got %q want nuclei", got)
	}
}

func TestDetectMissingExecutableFromShellOutput(t *testing.T) {
	t.Parallel()

	runErr := errors.New("exit status 127")
	output := []byte("bash: line 1: nikto: command not found\n")
	got := detectMissingExecutable("bash", runErr, output)
	if got != "nikto" {
		t.Fatalf("detectMissingExecutable shell output: got %q want nikto", got)
	}
}
