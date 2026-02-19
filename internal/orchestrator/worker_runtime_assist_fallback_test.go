package orchestrator

import "testing"

func TestApplyCommandTargetFallbackInjectsNetworkTarget(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	task := TaskSpec{Targets: []string{"192.168.50.0/24"}}

	args, injected, target := applyCommandTargetFallback(policy, task, "nmap", []string{"-sV"})
	if !injected {
		t.Fatalf("expected fallback target injection")
	}
	if target != "192.168.50.0/24" {
		t.Fatalf("unexpected injected target: %q", target)
	}
	if len(args) != 2 || args[1] != "192.168.50.0/24" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestApplyCommandTargetFallbackSkipsWhenTargetAlreadyPresent(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	task := TaskSpec{Targets: []string{"192.168.50.0/24"}}

	args, injected, _ := applyCommandTargetFallback(policy, task, "nmap", []string{"-sV", "192.168.50.10"})
	if injected {
		t.Fatalf("did not expect injection when command already has target")
	}
	if len(args) != 2 {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestApplyCommandTargetFallbackSkipsNonNetworkCommands(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	task := TaskSpec{Targets: []string{"192.168.50.0/24"}}

	args, injected, _ := applyCommandTargetFallback(policy, task, "ls", []string{"-la"})
	if injected {
		t.Fatalf("did not expect injection for non-network command")
	}
	if len(args) != 1 || args[0] != "-la" {
		t.Fatalf("unexpected args: %#v", args)
	}
}
