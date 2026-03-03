package cli

import (
	"github.com/Jawbreaker1/CodeHackBot/internal/scopeguard"
)

func (r *Runner) validateCommandScope(command string, args []string) error {
	if len(r.cfg.Scope.Networks) == 0 && len(r.cfg.Scope.Targets) == 0 && len(r.cfg.Scope.DenyTargets) == 0 {
		return nil
	}
	policy := scopeguard.New(append(append([]string{}, r.cfg.Scope.Networks...), r.cfg.Scope.Targets...), r.cfg.Scope.DenyTargets)
	return policy.ValidateCommand(command, args, scopeguard.ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	})
}
