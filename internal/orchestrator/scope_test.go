package orchestrator

import "testing"

func TestScopePolicyValidateTaskTargets(t *testing.T) {
	t.Parallel()

	policy := NewScopePolicy(Scope{
		Networks:    []string{"192.168.50.0/24"},
		DenyTargets: []string{"192.168.50.99"},
	})

	allowed := task("t1", nil, 1)
	allowed.Targets = []string{"192.168.50.10"}
	if err := policy.ValidateTaskTargets(allowed); err != nil {
		t.Fatalf("expected allowed target, got %v", err)
	}

	denied := task("t2", nil, 1)
	denied.Targets = []string{"192.168.50.99"}
	if err := policy.ValidateTaskTargets(denied); err == nil {
		t.Fatalf("expected deny target violation")
	}

	outOfScope := task("t3", nil, 1)
	outOfScope.Targets = []string{"10.0.0.8"}
	if err := policy.ValidateTaskTargets(outOfScope); err == nil {
		t.Fatalf("expected out-of-scope violation")
	}
}

func TestScopePolicyValidateCommandTargets(t *testing.T) {
	t.Parallel()

	policy := NewScopePolicy(Scope{
		Networks:    []string{"192.168.50.0/24"},
		Targets:     []string{"systemverification.com"},
		DenyTargets: []string{"192.168.50.99"},
	})

	if err := policy.ValidateCommandTargets("nmap", []string{"-sV", "192.168.50.10"}); err != nil {
		t.Fatalf("expected allowed command target, got %v", err)
	}
	if err := policy.ValidateCommandTargets("curl", []string{"https://systemverification.com"}); err != nil {
		t.Fatalf("expected allowed literal target, got %v", err)
	}
	if err := policy.ValidateCommandTargets("nmap", []string{"-sV", "10.0.0.8"}); err == nil {
		t.Fatalf("expected out-of-scope command target violation")
	}
	if err := policy.ValidateCommandTargets("nmap", []string{"-sV", "192.168.50.99"}); err == nil {
		t.Fatalf("expected denied command target violation")
	}
}
