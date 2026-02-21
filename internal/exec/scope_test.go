package exec

import (
	"runtime"
	"strings"
	"testing"
)

func TestRunCommandScopeDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: echo may not be available as an external command")
	}
	runner := Runner{
		Permissions:      PermissionAll,
		ScopeDenyTargets: []string{"10.0.0.1"},
	}
	_, err := runner.RunCommand("echo", "10.0.0.1")
	if err == nil || !strings.Contains(err.Error(), "scope violation") {
		t.Fatalf("expected scope violation, got %v", err)
	}
}

func TestRunCommandScopeAllowNetwork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: echo may not be available as an external command")
	}
	runner := Runner{
		Permissions:   PermissionAll,
		ScopeNetworks: []string{"10.0.0.0/8"},
	}
	if _, err := runner.RunCommand("echo", "10.1.2.3"); err != nil {
		t.Fatalf("expected allowed command, got %v", err)
	}
}

func TestRunCommandScopeOutOfBounds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: echo may not be available as an external command")
	}
	runner := Runner{
		Permissions:   PermissionAll,
		ScopeNetworks: []string{"10.0.0.0/8"},
	}
	_, err := runner.RunCommand("echo", "8.8.8.8")
	if err == nil || !strings.Contains(err.Error(), "scope violation") {
		t.Fatalf("expected scope violation, got %v", err)
	}
}

func TestRunCommandScopeInternalAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: echo may not be available as an external command")
	}
	runner := Runner{
		Permissions:   PermissionAll,
		ScopeNetworks: []string{"internal"},
	}
	if _, err := runner.RunCommand("echo", "10.2.3.4"); err != nil {
		t.Fatalf("expected internal target allowed, got %v", err)
	}
	_, err := runner.RunCommand("echo", "8.8.8.8")
	if err == nil || !strings.Contains(err.Error(), "scope violation") {
		t.Fatalf("expected scope violation, got %v", err)
	}
}

func TestRunCommandScopeLiteralTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: echo may not be available as an external command")
	}
	runner := Runner{
		Permissions:  PermissionAll,
		ScopeTargets: []string{"example.local"},
	}
	if _, err := runner.RunCommand("echo", "http://example.local:8080"); err != nil {
		t.Fatalf("expected literal target allowed, got %v", err)
	}
}

func TestRunCommandScopeNoTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: echo may not be available as an external command")
	}
	runner := Runner{
		Permissions:   PermissionAll,
		ScopeNetworks: []string{"10.0.0.0/8"},
	}
	if _, err := runner.RunCommand("echo", "hello"); err != nil {
		t.Fatalf("expected no target to be allowed, got %v", err)
	}
}

func TestRunCommandScopeWrappedNetworkMissingTargetDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: bash may not be available")
	}
	runner := Runner{
		Permissions:   PermissionAll,
		ScopeNetworks: []string{"10.0.0.0/8"},
	}
	_, err := runner.RunCommand("bash", "-lc", "nmap -sV")
	if err == nil || !strings.Contains(err.Error(), "scope violation") {
		t.Fatalf("expected wrapped command scope violation, got %v", err)
	}
}

func TestRunCommandScopeWrappedNetworkInScopeAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: bash may not be available")
	}
	runner := Runner{
		Permissions:   PermissionAll,
		ScopeNetworks: []string{"10.0.0.0/8"},
	}
	if _, err := runner.RunCommand("bash", "-lc", "echo 10.1.2.3"); err != nil {
		t.Fatalf("expected wrapped in-scope command allowed, got %v", err)
	}
}

func TestRunCommandScopeWrappedNetworkOutOfScopeDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: bash may not be available")
	}
	runner := Runner{
		Permissions:   PermissionAll,
		ScopeNetworks: []string{"10.0.0.0/8"},
	}
	_, err := runner.RunCommand("bash", "-lc", "echo 8.8.8.8")
	if err == nil || !strings.Contains(err.Error(), "scope violation") {
		t.Fatalf("expected wrapped out-of-scope command denied, got %v", err)
	}
}

func TestRunCommandScopeWrappedNetworkDeniedTargetDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: bash may not be available")
	}
	runner := Runner{
		Permissions:      PermissionAll,
		ScopeNetworks:    []string{"10.0.0.0/8"},
		ScopeDenyTargets: []string{"10.0.0.9"},
	}
	_, err := runner.RunCommand("bash", "-lc", "echo 10.0.0.9")
	if err == nil || !strings.Contains(err.Error(), "scope violation") {
		t.Fatalf("expected wrapped denied target to fail, got %v", err)
	}
}
