package orchestrator

import (
	"strings"
	"testing"
)

func TestTrackAssistActionStreakResetsOnNewAction(t *testing.T) {
	t.Parallel()

	key, streak := trackAssistActionStreak("", 0, "nmap\x1f-sV\x1f192.168.50.1")
	if key == "" || streak != 1 {
		t.Fatalf("expected initial streak 1, got key=%q streak=%d", key, streak)
	}

	key, streak = trackAssistActionStreak(key, streak, "nmap\x1f-sV\x1f192.168.50.1")
	if streak != 2 {
		t.Fatalf("expected repeated streak 2, got %d", streak)
	}

	key, streak = trackAssistActionStreak(key, streak, "grep\x1frouter")
	if key != "grep\x1frouter" || streak != 1 {
		t.Fatalf("expected streak reset for new action, got key=%q streak=%d", key, streak)
	}
}

func TestTrackAssistActionStreakEmptyActionClearsState(t *testing.T) {
	t.Parallel()

	key, streak := trackAssistActionStreak("nmap\x1f-sV", 2, "")
	if key != "" || streak != 0 {
		t.Fatalf("expected empty action to clear streak, got key=%q streak=%d", key, streak)
	}
}

func TestBuildAssistActionKeyCanonicalizesAliasAndArgOrder(t *testing.T) {
	t.Parallel()

	keyA := buildAssistActionKey("ls", []string{"-la", "."})
	keyB := buildAssistActionKey("list_dir", []string{".", "-la"})
	if keyA != keyB {
		t.Fatalf("expected semantic key match for alias/reordered args, got %q vs %q", keyA, keyB)
	}
}

func TestBuildAssistActionKeyKeepsArgOrderForOrderSensitiveCommands(t *testing.T) {
	t.Parallel()

	keyA := buildAssistActionKey("nmap", []string{"--script", "safe", "10.0.0.5"})
	keyB := buildAssistActionKey("nmap", []string{"10.0.0.5", "--script", "safe"})
	if keyA == keyB {
		t.Fatalf("expected different keys for reordered args on order-sensitive command")
	}
}

func TestIsNoNewEvidenceCandidateShellScripts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		command string
		args    []string
		want    bool
	}{
		{
			name:    "network command",
			command: "nmap",
			args:    []string{"127.0.0.1"},
			want:    true,
		},
		{
			name:    "tools relative script",
			command: "bash",
			args:    []string{"tools/check_loopback.sh"},
			want:    true,
		},
		{
			name:    "bare shell script",
			command: "bash",
			args:    []string{"check_loopback.sh"},
			want:    true,
		},
		{
			name:    "absolute shell script",
			command: "bash",
			args:    []string{"/tmp/tools/validate_loopback.sh"},
			want:    true,
		},
		{
			name:    "shell inline command",
			command: "bash",
			args:    []string{"-lc", "echo ok"},
			want:    false,
		},
		{
			name:    "list dir builtin",
			command: "list_dir",
			args:    []string{"."},
			want:    true,
		},
		{
			name:    "read file builtin",
			command: "read_file",
			args:    []string{"secret.zip"},
			want:    true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isNoNewEvidenceCandidate(tc.command, tc.args)
			if got != tc.want {
				t.Fatalf("isNoNewEvidenceCandidate(%q, %v) = %v, want %v", tc.command, tc.args, got, tc.want)
			}
		})
	}
}

func TestNormalizeWorkerAssistCommandRewritesSingleArgShellCommand(t *testing.T) {
	t.Parallel()

	command, args := normalizeWorkerAssistCommand("bash", []string{"nmap -sV -p- 127.0.0.1"})
	if command != "bash" {
		t.Fatalf("expected command bash, got %q", command)
	}
	if len(args) != 2 || args[0] != "-lc" || args[1] != "nmap -sV -p- 127.0.0.1" {
		t.Fatalf("expected -lc wrapped shell command, got %#v", args)
	}
}

func TestNormalizeWorkerAssistCommandKeepsSingleArgShellScript(t *testing.T) {
	t.Parallel()

	command, args := normalizeWorkerAssistCommand("bash", []string{"scan.sh"})
	if command != "bash" {
		t.Fatalf("expected command bash, got %q", command)
	}
	if len(args) != 1 || args[0] != "scan.sh" {
		t.Fatalf("expected shell script arg unchanged, got %#v", args)
	}
}

func TestNormalizeWorkerAssistCommandSplitsInlineFlagsWithArgs(t *testing.T) {
	t.Parallel()

	command, args := normalizeWorkerAssistCommand("ls -la", []string{"."})
	if command != "ls" {
		t.Fatalf("expected command ls, got %q", command)
	}
	if len(args) != 2 || args[0] != "-la" || args[1] != "." {
		t.Fatalf("expected args [-la .], got %#v", args)
	}
}

func TestNormalizeWorkerAssistCommandSplitsShellPrefixWithScriptArg(t *testing.T) {
	t.Parallel()

	command, args := normalizeWorkerAssistCommand("bash -c", []string{"echo ok"})
	if command != "bash" {
		t.Fatalf("expected command bash, got %q", command)
	}
	if len(args) != 2 || args[0] != "-c" || args[1] != "echo ok" {
		t.Fatalf("expected args [-c \"echo ok\"], got %#v", args)
	}
}

func TestNormalizeWorkerAssistCommandNormalizesMetasploitExecPayload(t *testing.T) {
	t.Parallel()

	command, args := normalizeWorkerAssistCommand("msfconsole -q -x \"use auxiliary/scanner/http/http_version", []string{"set", "RHOSTS", "192.168.50.1;", "run;", "exit\""})
	if command != "msfconsole" {
		t.Fatalf("expected command msfconsole, got %q", command)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %#v", args)
	}
	if args[0] != "-q" || args[1] != "-x" {
		t.Fatalf("unexpected metasploit args: %#v", args)
	}
	if args[2] != "use auxiliary/scanner/http/http_version set RHOSTS 192.168.50.1; run; exit" {
		t.Fatalf("unexpected -x payload: %q", args[2])
	}
}

func TestNormalizeWorkerAssistCommandWrapsShellOperatorArgs(t *testing.T) {
	t.Parallel()

	command, args := normalizeWorkerAssistCommand("msfconsole", []string{"-x", "use auxiliary/scanner/http/http_version; set RHOSTS 192.168.50.1; run; exit", "|", "tee", "msf.log"})
	if command != "bash" {
		t.Fatalf("expected bash wrapper, got %q", command)
	}
	if len(args) != 2 || args[0] != "-lc" {
		t.Fatalf("expected bash -lc wrapper args, got %#v", args)
	}
	if !strings.Contains(args[1], "msfconsole") || !strings.Contains(args[1], "| tee") {
		t.Fatalf("unexpected wrapped script: %q", args[1])
	}
}

func TestNormalizeWorkerAssistCommandKeepsRegexPipeArg(t *testing.T) {
	t.Parallel()

	command, args := normalizeWorkerAssistCommand("grep", []string{"-E", "(HTTP|version)", "scan.log"})
	if command != "grep" {
		t.Fatalf("expected grep command unchanged, got %q", command)
	}
	if len(args) != 3 || args[1] != "(HTTP|version)" {
		t.Fatalf("expected regex arg preserved, got %#v", args)
	}
}
