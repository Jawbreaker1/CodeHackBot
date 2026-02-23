package orchestrator

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyWorkerRuntimeGuardrailsAddsNmapWrapperToPath(t *testing.T) {
	prev := lookPath
	lookPath = func(file string) (string, error) {
		if file == "nmap" {
			return "/usr/bin/nmap", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = prev })

	workDir := t.TempDir()
	baseEnv := []string{"PATH=/usr/bin:/bin", "HOME=/tmp"}
	env, notes, err := applyWorkerRuntimeGuardrails(baseEnv, workDir)
	if err != nil {
		t.Fatalf("applyWorkerRuntimeGuardrails returned error: %v", err)
	}
	if len(notes) == 0 {
		t.Fatalf("expected guardrail note")
	}

	binDir := filepath.Join(workDir, workerRuntimeGuardrailBinRelPath)
	pathValue := envValue(env, "PATH")
	if !strings.HasPrefix(pathValue, binDir+string(os.PathListSeparator)) {
		t.Fatalf("expected PATH prefixed with guardrail bin dir, got %q", pathValue)
	}

	wrapperPath := filepath.Join(binDir, "nmap")
	content, readErr := os.ReadFile(wrapperPath)
	if readErr != nil {
		t.Fatalf("expected nmap wrapper file: %v", readErr)
	}
	text := string(content)
	if !strings.Contains(text, `real_nmap="/usr/bin/nmap"`) {
		t.Fatalf("wrapper should reference real nmap path, got: %s", text)
	}
	if !strings.Contains(text, `prefix+=("--max-rate" "1500")`) {
		t.Fatalf("wrapper should enforce max-rate guardrail, got: %s", text)
	}
}

func TestApplyWorkerRuntimeGuardrailsNoopWhenNmapMissing(t *testing.T) {
	prev := lookPath
	lookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = prev })

	baseEnv := []string{"PATH=/usr/bin:/bin", "HOME=/tmp"}
	env, notes, err := applyWorkerRuntimeGuardrails(baseEnv, t.TempDir())
	if err != nil {
		t.Fatalf("applyWorkerRuntimeGuardrails returned error: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected no notes when nmap is unavailable, got %#v", notes)
	}
	if strings.Join(env, "\n") != strings.Join(baseEnv, "\n") {
		t.Fatalf("expected env unchanged when nmap missing, got %#v", env)
	}
}

func TestPrependPathEntryAvoidsDuplicates(t *testing.T) {
	t.Parallel()

	current := "/tmp/runtime-bin:/usr/bin:/bin"
	out := prependPathEntry(current, "/tmp/runtime-bin")
	if out != current {
		t.Fatalf("expected duplicate path prefix not to be added, got %q", out)
	}
}
