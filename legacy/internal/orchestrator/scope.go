package orchestrator

import "github.com/Jawbreaker1/CodeHackBot/internal/scopeguard"

type ScopePolicy struct {
	policy *scopeguard.Policy
}

func NewScopePolicy(scope Scope) *ScopePolicy {
	return &ScopePolicy{
		policy: scopeguard.New(append(scope.Networks, scope.Targets...), scope.DenyTargets),
	}
}

func (p *ScopePolicy) ValidateTaskTargets(task TaskSpec) error {
	if p == nil || p.policy == nil {
		return nil
	}
	return p.policy.ValidateTargets(task.Targets)
}

func (p *ScopePolicy) ValidateCommandTargets(command string, args []string) error {
	if p == nil || p.policy == nil {
		return nil
	}
	return p.policy.ValidateCommand(command, args, scopeguard.ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	})
}

func (p *ScopePolicy) FirstAllowedTarget() string {
	if p == nil || p.policy == nil {
		return ""
	}
	return p.policy.FirstAllowedTarget()
}

func (p *ScopePolicy) extractTargets(command string, args []string) []string {
	if p == nil || p.policy == nil {
		return nil
	}
	return p.policy.ExtractTargets(command, args)
}

func isNetworkSensitiveCommand(command string) bool {
	return scopeguard.IsNetworkSensitiveCommand(command)
}
