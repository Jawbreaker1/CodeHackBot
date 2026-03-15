package cli

import "testing"

func TestNormalizeShellScriptArgs(t *testing.T) {
	t.Run("strips outer single quotes for bash -lc", func(t *testing.T) {
		got := normalizeShellScriptArgs("bash", []string{"-lc", `'echo hi | grep -oP "(?<=h)i"'`})
		if len(got) != 2 {
			t.Fatalf("expected 2 args, got %d", len(got))
		}
		if got[1] != `echo hi | grep -oP "(?<=h)i"` {
			t.Fatalf("unexpected script: %q", got[1])
		}
	})

	t.Run("joins fragmented script args", func(t *testing.T) {
		got := normalizeShellScriptArgs("sh", []string{"-c", "echo", "hello", "world"})
		if len(got) != 2 {
			t.Fatalf("expected 2 args, got %d", len(got))
		}
		if got[1] != "echo hello world" {
			t.Fatalf("unexpected script: %q", got[1])
		}
	})

	t.Run("non shell command unchanged", func(t *testing.T) {
		in := []string{"-la"}
		got := normalizeShellScriptArgs("ls", in)
		if len(got) != len(in) || got[0] != in[0] {
			t.Fatalf("expected unchanged args: %v", got)
		}
	})
}
