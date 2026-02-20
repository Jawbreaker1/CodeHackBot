package orchestrator

import (
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestAdaptCommandForRuntimeRemovesMissingAbsoluteInputListAndDowngradesSynScan(t *testing.T) {
	t.Parallel()

	prev := currentUserLookup
	currentUserLookup = func() (*user.User, error) {
		return &user.User{Uid: "1000"}, nil
	}
	t.Cleanup(func() { currentUserLookup = prev })

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	missingList := filepath.Join(t.TempDir(), "missing-live-hosts.txt")
	original := []string{"-sS", "-p-", "--open", "-iL", missingList, "192.168.50.0/24"}

	_, args, note, adapted := adaptCommandForRuntime(policy, "nmap", original)
	if !adapted {
		t.Fatalf("expected adaptation for missing -iL file and -sS")
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, missingList) || strings.Contains(joined, "-iL") {
		t.Fatalf("expected missing -iL args removed, got %#v", args)
	}
	if !strings.Contains(joined, "-sT") {
		t.Fatalf("expected -sS downgraded to -sT, got %#v", args)
	}
	if !strings.Contains(note, "removed missing nmap -iL file") || !strings.Contains(note, "downgraded nmap SYN scan") {
		t.Fatalf("expected combined adaptation note, got %q", note)
	}
}

func TestAdaptCommandForRuntimeKeepsRelativeInputListPath(t *testing.T) {
	t.Parallel()

	prev := currentUserLookup
	currentUserLookup = func() (*user.User, error) {
		return &user.User{Uid: "0"}, nil
	}
	t.Cleanup(func() { currentUserLookup = prev })

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	original := []string{"-Pn", "-p", "80", "-iL", "./live_hosts.txt"}

	_, args, _, adapted := adaptCommandForRuntime(policy, "nmap", original)
	if adapted {
		t.Fatalf("did not expect adaptation for relative -iL path that may resolve in worker workspace")
	}
	if len(args) != len(original) {
		t.Fatalf("expected args unchanged, got %#v", args)
	}
}
