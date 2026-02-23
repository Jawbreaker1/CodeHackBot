package orchestrator

import (
	"os"
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
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-sn") {
		t.Fatalf("expected host discovery args, got %#v", args)
	}
	if args[len(args)-1] != "192.168.50.0/24" {
		t.Fatalf("expected target preserved, got %#v", args)
	}
	if note == "" {
		t.Fatalf("expected adaptation note")
	}
}

func TestAdaptCommandForRuntimeEnforcesGuardrailsForHostScan(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	original := []string{"-sV", "-O", "192.168.50.10"}
	_, args, note, adapted := adaptCommandForRuntime(policy, "nmap", original)
	if !adapted {
		t.Fatalf("expected guardrail adaptation for host scan")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-n") || !strings.Contains(joined, "--host-timeout 45s") || !strings.Contains(joined, "--max-retries 2") || !strings.Contains(joined, "--max-rate 1200") {
		t.Fatalf("expected nmap guardrails in args, got %#v", args)
	}
	if !strings.Contains(note, "runtime guardrails") || !strings.Contains(note, "profile=service_enum") {
		t.Fatalf("expected runtime guardrail note, got %q", note)
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
	if !strings.Contains(joined, "-sn") {
		t.Fatalf("expected broad scan downshifted to host discovery, got %#v", args)
	}
	if !strings.Contains(note, "removed missing nmap -iL file") || !strings.Contains(note, "host discovery") {
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
	if !adapted {
		t.Fatalf("expected guardrail adaptation for relative -iL path")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-n") || !strings.Contains(joined, "--max-rate 1500") {
		t.Fatalf("expected guardrail flags, got %#v", args)
	}
}

func TestAdaptCommandForRuntimeDownshiftsBroadNmapAllPortsScan(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	original := []string{"-sS", "-p-", "--open", "192.168.50.0/24"}

	_, args, note, adapted := adaptCommandForRuntime(policy, "nmap", original)
	if !adapted {
		t.Fatalf("expected adaptation for broad all-ports scan")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-sn") {
		t.Fatalf("expected host discovery args, got %#v", args)
	}
	if args[len(args)-1] != "192.168.50.0/24" {
		t.Fatalf("expected target preserved, got %#v", args)
	}
	if !strings.Contains(note, "host discovery") {
		t.Fatalf("expected host discovery adaptation note, got %q", note)
	}
}

func TestAdaptCommandForRuntimeCapsLoopbackDiscoveryRange(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"127.0.0.0/8"}, Targets: []string{"127.0.0.1"}}
	policy := NewScopePolicy(scope)
	original := []string{"-sn", "-n", "-oN", "nmap_scan_results.txt", "127.0.0.0/8"}

	_, args, note, adapted := adaptCommandForRuntime(policy, "nmap", original)
	if !adapted {
		t.Fatalf("expected adaptation for broad loopback discovery")
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "127.0.0.0/8") {
		t.Fatalf("expected broad loopback target removed, got %#v", args)
	}
	if !strings.Contains(joined, "127.0.0.1") {
		t.Fatalf("expected loopback discovery capped to 127.0.0.1, got %#v", args)
	}
	if !strings.Contains(note, "limit output volume") {
		t.Fatalf("expected output volume note, got %q", note)
	}
}

func TestAdaptCommandForRuntimeKeepsNonLoopbackDiscoveryRange(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"192.168.50.0/24"}}
	policy := NewScopePolicy(scope)
	original := []string{"-sn", "-n", "192.168.50.0/24"}

	_, args, _, adapted := adaptCommandForRuntime(policy, "nmap", original)
	if !adapted {
		t.Fatalf("expected guardrail adaptation for non-loopback discovery range")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--max-rate 1500") {
		t.Fatalf("expected rate guardrail, got %#v", args)
	}
}

func TestAdaptCommandForRuntimeSupportsAbsoluteNmapPath(t *testing.T) {
	t.Parallel()

	scope := Scope{Networks: []string{"127.0.0.0/8"}, Targets: []string{"127.0.0.1"}}
	policy := NewScopePolicy(scope)
	_, args, _, adapted := adaptCommandForRuntime(policy, "/usr/bin/nmap", []string{"-sn", "127.0.0.0/8"})
	if !adapted {
		t.Fatalf("expected adaptation for absolute nmap path")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "127.0.0.1") {
		t.Fatalf("expected loopback target cap for absolute nmap path, got %#v", args)
	}
}

func TestEnsureNmapRuntimeGuardrailsRemovesReverseDNSFlags(t *testing.T) {
	t.Parallel()

	args, note, adapted := ensureNmapRuntimeGuardrails([]string{"-R", "--system-dns", "-sV", "127.0.0.1"})
	if !adapted {
		t.Fatalf("expected reverse DNS flags removed")
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "-R") || strings.Contains(joined, "--system-dns") {
		t.Fatalf("expected reverse DNS flags removed, got %#v", args)
	}
	if !strings.Contains(joined, "-n") {
		t.Fatalf("expected -n added, got %#v", args)
	}
	if !strings.Contains(note, "DNS-safe") {
		t.Fatalf("expected reverse-DNS note, got %q", note)
	}
}

func TestEnsureNmapRuntimeGuardrailsProfiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		args              []string
		wantHostTimeout   string
		wantMaxRetries    string
		wantMaxRate       string
		wantProfileInNote string
	}{
		{
			name:              "discovery",
			args:              []string{"-sn", "127.0.0.1"},
			wantHostTimeout:   "10s",
			wantMaxRetries:    "1",
			wantMaxRate:       "1500",
			wantProfileInNote: "profile=discovery",
		},
		{
			name:              "service_enum",
			args:              []string{"-sV", "127.0.0.1"},
			wantHostTimeout:   "45s",
			wantMaxRetries:    "2",
			wantMaxRate:       "1200",
			wantProfileInNote: "profile=service_enum",
		},
		{
			name:              "vuln_mapping",
			args:              []string{"--script", "vuln", "-sV", "127.0.0.1"},
			wantHostTimeout:   "90s",
			wantMaxRetries:    "2",
			wantMaxRate:       "800",
			wantProfileInNote: "profile=vuln_mapping",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args, note, adapted := ensureNmapRuntimeGuardrails(tc.args)
			if !adapted {
				t.Fatalf("expected guardrail adaptation")
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "--host-timeout "+tc.wantHostTimeout) {
				t.Fatalf("expected host-timeout %s in args: %#v", tc.wantHostTimeout, args)
			}
			if !strings.Contains(joined, "--max-retries "+tc.wantMaxRetries) {
				t.Fatalf("expected max-retries %s in args: %#v", tc.wantMaxRetries, args)
			}
			if !strings.Contains(joined, "--max-rate "+tc.wantMaxRate) {
				t.Fatalf("expected max-rate %s in args: %#v", tc.wantMaxRate, args)
			}
			if !strings.Contains(note, tc.wantProfileInNote) {
				t.Fatalf("expected profile note %q, got %q", tc.wantProfileInNote, note)
			}
		})
	}
}

func TestAdaptCommandForRuntimeCapsServiceTopPortsAndUsesVersionLight(t *testing.T) {
	t.Parallel()

	scope := Scope{Targets: []string{"127.0.0.1"}}
	policy := NewScopePolicy(scope)
	_, args, note, adapted := adaptCommandForRuntime(policy, "nmap", []string{"-sV", "--top-ports", "100", "127.0.0.1"})
	if !adapted {
		t.Fatalf("expected adaptation for bounded service-enum profile")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--top-ports 20") {
		t.Fatalf("expected top-ports cap, got %#v", args)
	}
	if !strings.Contains(joined, "--version-light") {
		t.Fatalf("expected --version-light for bounded service enumeration, got %#v", args)
	}
	if !strings.Contains(note, "port breadth") || !strings.Contains(note, "version-light") {
		t.Fatalf("expected bounded service-enum notes, got %q", note)
	}
}

func TestAdaptCommandForRuntimeRewritesVulnScriptToSafeSubset(t *testing.T) {
	t.Parallel()

	scope := Scope{Targets: []string{"127.0.0.1"}}
	policy := NewScopePolicy(scope)
	_, args, note, adapted := adaptCommandForRuntime(policy, "nmap", []string{"--script", "vuln", "-sV", "127.0.0.1"})
	if !adapted {
		t.Fatalf("expected adaptation for vuln mapping bounds")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--script vuln and safe") {
		t.Fatalf("expected vuln script rewrite, got %#v", args)
	}
	if !strings.Contains(joined, "--script-timeout 20s") {
		t.Fatalf("expected script timeout bound, got %#v", args)
	}
	if !strings.Contains(joined, "--top-ports 20") {
		t.Fatalf("expected top-ports bound for vuln mapping, got %#v", args)
	}
	if !strings.Contains(note, "vuln and safe") || !strings.Contains(note, "script-timeout") {
		t.Fatalf("expected vuln-bounds note, got %q", note)
	}
}

func TestAdaptCommandForRuntimeSanitizesUnsupportedNmapScriptNames(t *testing.T) {
	t.Parallel()

	tempScripts := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempScripts, "http-auth.nse"), []byte("description"), 0o644); err != nil {
		t.Fatalf("WriteFile http-auth.nse: %v", err)
	}

	prevDirs := append([]string{}, nmapScriptDirs...)
	nmapScriptDirs = []string{tempScripts}
	t.Cleanup(func() { nmapScriptDirs = prevDirs })

	scope := Scope{Targets: []string{"127.0.0.1"}}
	policy := NewScopePolicy(scope)
	_, args, note, adapted := adaptCommandForRuntime(policy, "nmap", []string{
		"--script", "http-auth,http-sessionparse,vuln",
		"127.0.0.1",
	})
	if !adapted {
		t.Fatalf("expected adaptation for unsupported --script entries")
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "http-sessionparse") {
		t.Fatalf("expected unsupported script removed, got %#v", args)
	}
	if !strings.Contains(joined, "http-auth") || !strings.Contains(joined, "vuln") {
		t.Fatalf("expected supported scripts retained, got %#v", args)
	}
	if !strings.Contains(note, "unsupported nmap --script entries") {
		t.Fatalf("expected unsupported script note, got %q", note)
	}
}
