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

func TestValidateCommandExtractsURLHostFromWrappedScriptWithPunctuation(t *testing.T) {
	t.Parallel()

	policy := New([]string{"scanme.nmap.org"}, nil)
	if err := policy.ValidateCommand("bash", []string{"-lc", "(curl -k -I https://scanme.nmap.org || curl -I http://scanme.nmap.org) | tee headers.txt"}, ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	}); err != nil {
		t.Fatalf("expected wrapped URL host extraction with punctuation to be allowed, got %v", err)
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

func TestValidateCommandIgnoresLocalScriptFileAsHostTarget(t *testing.T) {
	t.Parallel()

	policy := New([]string{"127.0.0.0/8"}, nil)
	if err := policy.ValidateCommand("bash", []string{"scan.sh"}, ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	}); err != nil {
		t.Fatalf("expected local script argument not treated as out-of-scope host, got %v", err)
	}
}

func TestValidateCommandIgnoresLocalHashArtifactAsHostTarget(t *testing.T) {
	t.Parallel()

	policy := New([]string{"127.0.0.0/8"}, nil)
	if err := policy.ValidateCommand("john", []string{"--format=zip", "zip.hash"}, ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	}); err != nil {
		t.Fatalf("expected local hash artifact argument not treated as out-of-scope host, got %v", err)
	}
}

func TestValidateCommandWrapperIgnoresArchiveExtractionScriptAsNetworkIntent(t *testing.T) {
	t.Parallel()

	policy := New([]string{"127.0.0.0/8"}, nil)
	if err := policy.ValidateCommand("bash", []string{"-lc", "unzip -P $(cat password_found) secret.zip"}, ValidateOptions{
		FailClosedNetwork:  true,
		FailClosedWrappers: true,
	}); err != nil {
		t.Fatalf("expected local archive extraction wrapper not treated as network command, got %v", err)
	}
}
