package msf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdaptRuntimeCommand_RewritesMSFConsoleExecuteFlag(t *testing.T) {
	t.Parallel()

	cmd, args, notes, err := AdaptRuntimeCommand("msfconsole", []string{"-q", "-e", "db_status; exit"}, "")
	if err != nil {
		t.Fatalf("AdaptRuntimeCommand: %v", err)
	}
	if cmd != "msfconsole" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if len(args) < 3 || args[1] != "-x" {
		t.Fatalf("expected -x flag, got %#v", args)
	}
	if len(notes) == 0 {
		t.Fatalf("expected adaptation note")
	}
}

func TestAdaptRuntimeCommand_PatchesShellScriptMSFFlag(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "check_msf.sh")
	content := "#!/usr/bin/env bash\nmsfconsole -q -e \"db_status; exit\"\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd, args, notes, err := AdaptRuntimeCommand("bash", []string{scriptPath}, tmp)
	if err != nil {
		t.Fatalf("AdaptRuntimeCommand: %v", err)
	}
	if cmd != "bash" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if len(args) != 1 || args[0] != scriptPath {
		t.Fatalf("unexpected args: %#v", args)
	}
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read patched script: %v", err)
	}
	if strings.Contains(string(data), " -e ") {
		t.Fatalf("expected script patched to -x, got:\n%s", string(data))
	}
	if len(notes) == 0 {
		t.Fatalf("expected patch note")
	}
}

func TestAdaptRuntimeCommand_AdaptsRubyMetasploitScript(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "msf_exploit.rb")
	content := "require 'msf/core'\nputs 'x'\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write ruby script: %v", err)
	}

	cmd, args, notes, err := AdaptRuntimeCommand("ruby", []string{"msf_exploit.rb"}, tmp)
	if err != nil {
		t.Fatalf("AdaptRuntimeCommand: %v", err)
	}
	if cmd != "bash" {
		t.Fatalf("expected bash adaptation, got %q", cmd)
	}
	if len(args) != 2 || args[0] != "-lc" {
		t.Fatalf("unexpected args: %#v", args)
	}
	if !strings.Contains(args[1], "msfenv") || !strings.Contains(args[1], "ruby ") {
		t.Fatalf("expected msfenv bootstrap expression, got: %s", args[1])
	}
	if len(notes) == 0 {
		t.Fatalf("expected adaptation note")
	}
}

func TestAdaptRuntimeCommand_SkipsPlainRubyScript(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "plain.rb")
	if err := os.WriteFile(scriptPath, []byte("puts 'ok'\n"), 0o644); err != nil {
		t.Fatalf("write ruby script: %v", err)
	}

	cmd, args, notes, err := AdaptRuntimeCommand("ruby", []string{"plain.rb"}, tmp)
	if err != nil {
		t.Fatalf("AdaptRuntimeCommand: %v", err)
	}
	if cmd != "ruby" {
		t.Fatalf("expected ruby unchanged, got %q", cmd)
	}
	if len(args) != 1 || args[0] != "plain.rb" {
		t.Fatalf("unexpected args: %#v", args)
	}
	if len(notes) != 0 {
		t.Fatalf("expected no notes for plain ruby script, got %#v", notes)
	}
}
