package orchestrator

import (
	"strings"
	"testing"
)

func TestNormalizeCommandActionNormalizesMetasploitExecPayload(t *testing.T) {
	t.Parallel()

	command, args := normalizeCommandAction("msfconsole", []string{"-q", "-x", "\"use auxiliary/scanner/http/http_version; set RHOSTS 192.168.50.1; run; exit\""})
	if command != "msfconsole" {
		t.Fatalf("expected command msfconsole, got %q", command)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %#v", args)
	}
	if args[2] != "use auxiliary/scanner/http/http_version; set RHOSTS 192.168.50.1; run; exit" {
		t.Fatalf("unexpected -x payload: %q", args[2])
	}
}

func TestNormalizeCommandActionParsesInlineMetasploitCommand(t *testing.T) {
	t.Parallel()

	command, args := normalizeCommandAction(`msfconsole -q -x "use auxiliary/scanner/http/http_version; set RHOSTS 192.168.50.1; run; exit"`, nil)
	if command != "msfconsole" {
		t.Fatalf("expected command msfconsole, got %q", command)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %#v", args)
	}
	if args[0] != "-q" || args[1] != "-x" {
		t.Fatalf("unexpected args prefix: %#v", args)
	}
	if args[2] != "use auxiliary/scanner/http/http_version; set RHOSTS 192.168.50.1; run; exit" {
		t.Fatalf("unexpected -x payload: %q", args[2])
	}
}

func TestNormalizeCommandActionWrapsShellOperatorArgs(t *testing.T) {
	t.Parallel()

	command, args := normalizeCommandAction("msfconsole", []string{"-x", "use auxiliary/scanner/http/http_version; set RHOSTS 192.168.50.1; run; exit", "|", "tee", "msf.log"})
	if command != "bash" {
		t.Fatalf("expected bash wrapper, got %q", command)
	}
	if len(args) != 2 || args[0] != "-lc" {
		t.Fatalf("expected bash -lc wrapper args, got %#v", args)
	}
	if got := args[1]; got == "" || !containsAll(got, "msfconsole", "| tee", "msf.log") {
		t.Fatalf("unexpected wrapped script: %q", got)
	}
}

func TestNormalizeCommandActionKeepsRegexPipesAsNormalArgs(t *testing.T) {
	t.Parallel()

	command, args := normalizeCommandAction("grep", []string{"-E", "(HTTP|version)", "scan.log"})
	if command != "grep" {
		t.Fatalf("expected grep command unchanged, got %q", command)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %#v", args)
	}
	if args[1] != "(HTTP|version)" {
		t.Fatalf("expected regex arg preserved, got %q", args[1])
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
