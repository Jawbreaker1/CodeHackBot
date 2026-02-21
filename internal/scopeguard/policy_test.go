package scopeguard

import (
	"strings"
	"testing"
)

func TestValidateCommandWrapperFailClosedWhenTargetMissing(t *testing.T) {
	t.Parallel()

	policy := New([]string{"192.168.50.0/24"}, nil)
	err := policy.ValidateCommand("bash", []string{"-lc", "nmap -sV -Pn"}, ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	})
	if err == nil || !strings.Contains(err.Error(), "scope violation") {
		t.Fatalf("expected scope violation for wrapped network command without target, got %v", err)
	}
}

func TestValidateCommandWrapperInScopeAndOutOfScope(t *testing.T) {
	t.Parallel()

	policy := New([]string{"192.168.50.0/24"}, nil)
	if err := policy.ValidateCommand("bash", []string{"-lc", "nmap -sV 192.168.50.10"}, ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	}); err != nil {
		t.Fatalf("expected wrapped in-scope target allowed, got %v", err)
	}
	err := policy.ValidateCommand("bash", []string{"-lc", "nmap -sV 8.8.8.8"}, ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	})
	if err == nil || !strings.Contains(err.Error(), "scope violation") {
		t.Fatalf("expected wrapped out-of-scope target denied, got %v", err)
	}
}

func TestValidateCommandExtractsURLHost(t *testing.T) {
	t.Parallel()

	policy := New([]string{"systemverification.com"}, nil)
	if err := policy.ValidateCommand("bash", []string{"-lc", "curl -s https://systemverification.com/about"}, ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	}); err != nil {
		t.Fatalf("expected url host extraction to be allowed, got %v", err)
	}
}

func TestValidateCommandWrapperDeniedTarget(t *testing.T) {
	t.Parallel()

	policy := New([]string{"192.168.50.0/24"}, []string{"192.168.50.99"})
	err := policy.ValidateCommand("bash", []string{"-lc", "nmap -sV 192.168.50.99"}, ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	})
	if err == nil || !strings.Contains(err.Error(), "scope violation") {
		t.Fatalf("expected wrapped denied target violation, got %v", err)
	}
}
