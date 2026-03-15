package exec

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func TestScopePolicyParityWithOrchestrator(t *testing.T) {
	t.Parallel()

	networks := []string{"192.168.50.0/24"}
	targets := []string{"systemverification.com"}
	deny := []string{"192.168.50.99"}
	cases := []struct {
		name string
		cmd  string
		args []string
	}{
		{
			name: "direct in-scope ip",
			cmd:  "nmap",
			args: []string{"-sV", "192.168.50.10"},
		},
		{
			name: "direct out-of-scope ip",
			cmd:  "nmap",
			args: []string{"-sV", "8.8.8.8"},
		},
		{
			name: "direct denied ip",
			cmd:  "nmap",
			args: []string{"-sV", "192.168.50.99"},
		},
		{
			name: "wrapped missing target",
			cmd:  "bash",
			args: []string{"-lc", "nmap -sV"},
		},
		{
			name: "wrapped in-scope target",
			cmd:  "bash",
			args: []string{"-lc", "nmap -sV 192.168.50.11"},
		},
		{
			name: "wrapped denied target",
			cmd:  "bash",
			args: []string{"-lc", "nmap -sV 192.168.50.99"},
		},
		{
			name: "wrapped url target",
			cmd:  "bash",
			args: []string{"-lc", "curl -s https://systemverification.com"},
		},
	}

	runner := Runner{
		ScopeNetworks:    networks,
		ScopeTargets:     targets,
		ScopeDenyTargets: deny,
	}
	policy := orchestrator.NewScopePolicy(orchestrator.Scope{
		Networks:    networks,
		Targets:     targets,
		DenyTargets: deny,
	})

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			execErr := runner.validateScope(tc.cmd, tc.args)
			orchestratorErr := policy.ValidateCommandTargets(tc.cmd, tc.args)
			execDenied := execErr != nil
			orchestratorDenied := orchestratorErr != nil
			if execDenied != orchestratorDenied {
				t.Fatalf("scope parity mismatch: execErr=%v orchestratorErr=%v", execErr, orchestratorErr)
			}
		})
	}
}
