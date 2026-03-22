package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitialTaskRuntimeInfersTarget(t *testing.T) {
	got := InitialTaskRuntime("Extract contents of secret.zip and identify the password needed to decrypt it.")
	if got.State != "running" {
		t.Fatalf("State = %q", got.State)
	}
	if got.CurrentTarget != "secret.zip" {
		t.Fatalf("CurrentTarget = %q", got.CurrentTarget)
	}
}

func TestInitialTaskRuntimeInDirResolvesConcreteLocalTarget(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "secret.zip")
	if err := os.WriteFile(targetPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got := InitialTaskRuntimeInDir("Extract contents of secret.zip and identify the password needed to decrypt it.", dir)
	if got.CurrentTarget != targetPath {
		t.Fatalf("CurrentTarget = %q, want %q", got.CurrentTarget, targetPath)
	}
}

func TestUpdateTaskRuntimeUsesSignals(t *testing.T) {
	got := UpdateTaskRuntime(
		TaskRuntime{State: "running", CurrentTarget: "secret.zip"},
		"Extract secret.zip",
		ExecutionResult{
			Action:        "unzip -t secret.zip",
			ExitStatus:    "0",
			OutputSummary: "stdout: unable to get password",
			Assessment:    "suspicious",
			Signals:       []string{"incorrect_password"},
		},
	)
	if got.MissingFact != "credential or password required for secret.zip" {
		t.Fatalf("MissingFact = %q", got.MissingFact)
	}
}

func TestUpdateTaskRuntimeDefaultsToNextEvidenceAboutTarget(t *testing.T) {
	got := UpdateTaskRuntime(
		TaskRuntime{State: "running", CurrentTarget: "127.0.0.1"},
		"Perform local reconnaissance of 127.0.0.1",
		ExecutionResult{
			Action:        "nmap -sV 127.0.0.1",
			ExitStatus:    "0",
			OutputSummary: "stdout: 22/tcp open ssh OpenSSH 10.2p1 Debian 3",
			Assessment:    "success",
		},
	)
	if got.MissingFact != "next evidence needed about 127.0.0.1" {
		t.Fatalf("MissingFact = %q", got.MissingFact)
	}
}

func TestUpdateTaskRuntimeMissingPathPrefersFailingArtifactOverCurrentTarget(t *testing.T) {
	got := UpdateTaskRuntime(
		TaskRuntime{State: "running", CurrentTarget: "/tmp/fixture/secret.zip"},
		"Extract secret.zip",
		ExecutionResult{
			Action:        "fcrackzip -u -D -p /usr/share/wordlists/rockyou.txt secret.zip",
			ExitStatus:    "1",
			OutputSummary: "stdout: /usr/share/wordlists/rockyou.txt: No such file or directory",
			Assessment:    "failed",
			Signals:       []string{"nonzero_exit", "missing_path"},
		},
	)
	if got.MissingFact != "correct path or artifact needed: /usr/share/wordlists/rockyou.txt" {
		t.Fatalf("MissingFact = %q", got.MissingFact)
	}
}

func TestUpdateTaskRuntimeMissingPathFallsBackWhenOnlyTargetIsPresent(t *testing.T) {
	got := UpdateTaskRuntime(
		TaskRuntime{State: "running", CurrentTarget: "secret.zip"},
		"Extract secret.zip",
		ExecutionResult{
			Action:        "cat secret.zip",
			ExitStatus:    "1",
			OutputSummary: "stdout: missing path",
			Assessment:    "failed",
			Signals:       []string{"missing_path"},
		},
	)
	if got.MissingFact != "correct path or artifact needed for secret.zip" {
		t.Fatalf("MissingFact = %q", got.MissingFact)
	}
}

func TestUpdateTaskRuntimeMissingPathDoesNotPromoteUncorroboratedMalformedOutputPath(t *testing.T) {
	got := UpdateTaskRuntime(
		TaskRuntime{State: "running", CurrentTarget: "127.0.0.1"},
		"Perform local reconnaissance of 127.0.0.1",
		ExecutionResult{
			Action:        "cat /tmp/port_scan_$(ls -t /tmp/port_scan_*.log 2>/dev/null | head -1)",
			ExitStatus:    "1",
			OutputSummary: "stderr: cat: /tmp/port_scan_/tmp/port_scan_20260321_163003.log: No such file or directory",
			Assessment:    "failed",
			Signals:       []string{"nonzero_exit", "missing_path"},
		},
	)
	if got.MissingFact != "correct path or artifact needed" {
		t.Fatalf("MissingFact = %q", got.MissingFact)
	}
}

func TestInitialTaskRuntimeIgnoresSessionRuntimeArtifacts(t *testing.T) {
	got := InitialTaskRuntime("Inspect /tmp/session/logs/cmd-20260321.log and /tmp/session/context/step-001-pre-llm.txt before touching /tmp/session/session.json")
	if got.CurrentTarget != "" {
		t.Fatalf("CurrentTarget = %q, want empty", got.CurrentTarget)
	}
}

func TestInitialTaskRuntimeDoesNotIgnoreSessionFixtureFile(t *testing.T) {
	got := InitialTaskRuntime("Extract sessions/live-suite-20260321/zip-fixture/secret.zip")
	if got.CurrentTarget != "sessions/live-suite-20260321/zip-fixture/secret.zip" {
		t.Fatalf("CurrentTarget = %q", got.CurrentTarget)
	}
}

func TestInitialTaskRuntimeDoesNotIgnoreGenericLogsOrContextPaths(t *testing.T) {
	gotLogs := InitialTaskRuntime("Inspect /tmp/logs/report.zip")
	if gotLogs.CurrentTarget != "/tmp/logs/report.zip" {
		t.Fatalf("CurrentTarget(logs) = %q", gotLogs.CurrentTarget)
	}
	gotContext := InitialTaskRuntime("Inspect /srv/context/secret.txt")
	if gotContext.CurrentTarget != "/srv/context/secret.txt" {
		t.Fatalf("CurrentTarget(context) = %q", gotContext.CurrentTarget)
	}
}
