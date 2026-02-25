package orchestrator

import "testing"

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
