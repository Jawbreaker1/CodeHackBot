package orchestrator

import "testing"

func TestAdaptCommandForRuntimeDownshiftsBroadNmapFingerprint(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	command, args, note, adapted := adaptCommandForRuntime(policy, "nmap", []string{"-sV", "-O", "--osscan-guess", "192.168.50.0/24"})
	if !adapted {
		t.Fatalf("expected adaptation for broad CIDR fingerprint scan")
	}
	if command != "nmap" {
		t.Fatalf("unexpected command: %q", command)
	}
	if len(args) < 2 || args[0] != "-sn" {
		t.Fatalf("expected host discovery args, got %#v", args)
	}
	if args[len(args)-1] != "192.168.50.0/24" {
		t.Fatalf("expected target preserved, got %#v", args)
	}
	if note == "" {
		t.Fatalf("expected adaptation note")
	}
}

func TestAdaptCommandForRuntimeKeepsHostNmapUnchanged(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	original := []string{"-sV", "-O", "192.168.50.10"}
	_, args, _, adapted := adaptCommandForRuntime(policy, "nmap", original)
	if adapted {
		t.Fatalf("did not expect adaptation for host scan")
	}
	if len(args) != len(original) {
		t.Fatalf("unexpected args length: %#v", args)
	}
}
