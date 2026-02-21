package exec

import (
	"github.com/Jawbreaker1/CodeHackBot/internal/scopeguard"
)

func (r *Runner) validateScope(command string, args []string) error {
	if len(r.ScopeNetworks) == 0 && len(r.ScopeTargets) == 0 && len(r.ScopeDenyTargets) == 0 {
		return nil
	}
	policy := scopeguard.New(append(r.ScopeNetworks, r.ScopeTargets...), r.ScopeDenyTargets)
	return policy.ValidateCommand(command, args, scopeguard.ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	})
}
