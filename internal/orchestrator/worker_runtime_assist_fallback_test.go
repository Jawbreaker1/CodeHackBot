package orchestrator

import (
	"path/filepath"
	"strings"
	"testing"
)

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

func TestApplyCommandTargetFallbackUsesScopeDefaultWhenTaskHasNoTargets(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	task := TaskSpec{}

	args, injected, target := applyCommandTargetFallback(policy, task, "nmap", []string{"-sV"})
	if !injected {
		t.Fatalf("expected fallback target injection from scope")
	}
	if target != "192.168.50.0/24" {
		t.Fatalf("unexpected injected target: %q", target)
	}
	if len(args) != 2 || args[1] != "192.168.50.0/24" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestApplyCommandTargetFallbackReinjectsAfterRuntimeAdaptationDropsInputList(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"127.0.0.0/8"}}
	policy := NewScopePolicy(scope)
	task := TaskSpec{Targets: []string{"127.0.0.1"}}
	missingList := filepath.Join(t.TempDir(), "targets.txt")

	args := []string{"-sn", "-n", "--disable-arp-ping", "-oN", "nmap_scan_results.nmap", "-iL", missingList}
	args, injected, _ := applyCommandTargetFallback(policy, task, "nmap", args)
	if injected {
		t.Fatalf("expected initial fallback injection to be skipped with -iL present")
	}

	_, args, note, adapted := adaptCommandForRuntime(policy, "nmap", args)
	if !adapted {
		t.Fatalf("expected runtime adaptation for missing -iL file")
	}
	if !strings.Contains(note, "removed missing nmap -iL file") {
		t.Fatalf("expected -iL adaptation note, got %q", note)
	}

	args, injected, target := applyCommandTargetFallback(policy, task, "nmap", args)
	if !injected {
		t.Fatalf("expected fallback injection after runtime adaptation")
	}
	if target != "127.0.0.1" {
		t.Fatalf("unexpected reinjected target: %q", target)
	}
	if err := policy.ValidateCommandTargets("nmap", args); err != nil {
		t.Fatalf("expected reinjected command to pass scope validation: %v", err)
	}
}
