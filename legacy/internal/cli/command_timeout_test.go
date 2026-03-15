package cli

import (
	"testing"
	"time"
)

func TestResolveShellIdleTimeoutDefaults(t *testing.T) {
	got := resolveShellIdleTimeout(0, "echo", []string{"ok"})
	if got != defaultIdleTimeout {
		t.Fatalf("unexpected default timeout: got %s want %s", got, defaultIdleTimeout)
	}
}

func TestResolveShellIdleTimeoutNmapFloor(t *testing.T) {
	got := resolveShellIdleTimeout(5*time.Minute, "nmap", []string{"-sn", "192.168.50.0/24"})
	if got != longScanIdleTimeout {
		t.Fatalf("unexpected nmap timeout: got %s want %s", got, longScanIdleTimeout)
	}
}

func TestResolveShellIdleTimeoutKeepsHigherConfiguredValue(t *testing.T) {
	base := 30 * time.Minute
	got := resolveShellIdleTimeout(base, "nmap", []string{"-sn", "192.168.50.0/24"})
	if got != base {
		t.Fatalf("expected configured timeout to be preserved: got %s want %s", got, base)
	}
}

func TestClassifyCommandRecognizesShellWrappedCommands(t *testing.T) {
	if got := classifyCommand("bash", []string{"-lc", "nmap -sn 10.0.0.0/24"}); got != "nmap" {
		t.Fatalf("expected nmap classification, got %q", got)
	}
	if got := classifyCommand("sh", []string{"-c", "msfconsole -q -x 'search smb; exit -y'"}); got != "msfconsole" {
		t.Fatalf("expected msfconsole classification, got %q", got)
	}
}
