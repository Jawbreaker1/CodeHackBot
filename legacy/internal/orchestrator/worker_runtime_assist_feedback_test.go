package orchestrator

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderAssistExecutionFeedbackSectionIncludesCoreFields(t *testing.T) {
	t.Parallel()

	feedback := captureAssistExecutionFeedback(
		"msfconsole",
		[]string{"-x", "use auxiliary/scanner/http/http_version; run; exit"},
		errors.New("exit status 1"),
		[]byte("line1\nline2\nERROR: module failed\n"),
		"/tmp/run.log",
	)
	rendered := renderAssistExecutionFeedbackSection(feedback)
	if !strings.Contains(rendered, "[latest_execution_feedback]") {
		t.Fatalf("expected feedback section header, got: %q", rendered)
	}
	if !strings.Contains(rendered, "command: msfconsole") {
		t.Fatalf("expected command in feedback section, got: %q", rendered)
	}
	if !strings.Contains(rendered, "exit_code: 1") {
		t.Fatalf("expected exit code in feedback section, got: %q", rendered)
	}
	if !strings.Contains(rendered, "result_summary: command failed: msfconsole") {
		t.Fatalf("expected result summary in feedback section, got: %q", rendered)
	}
	if !strings.Contains(rendered, "artifact_log: /tmp/run.log") {
		t.Fatalf("expected artifact log in feedback section, got: %q", rendered)
	}
	if !strings.Contains(rendered, "runtime_error: exit status 1") {
		t.Fatalf("expected runtime error in feedback section, got: %q", rendered)
	}
	if !strings.Contains(rendered, "stderr_tail: unavailable") {
		t.Fatalf("expected stderr note in feedback section, got: %q", rendered)
	}
	if !strings.Contains(rendered, "ERROR: module failed") {
		t.Fatalf("expected output tail in feedback section, got: %q", rendered)
	}
}

func TestAppendAssistExecutionFeedbackToPromptAddsSection(t *testing.T) {
	t.Parallel()

	feedback := captureAssistExecutionFeedback("list_dir", []string{"."}, nil, []byte("a\nb\n"), "/tmp/list.log")
	recent, chat := appendAssistExecutionFeedbackToPrompt(&feedback, "[recent_artifacts]\n- /tmp/a.log", "[recent_actions]\n- ran list")
	if !strings.Contains(recent, "[latest_execution_feedback]") {
		t.Fatalf("expected feedback section in recent log, got: %q", recent)
	}
	if !strings.Contains(chat, "[latest_execution_feedback]") {
		t.Fatalf("expected feedback section in chat history, got: %q", chat)
	}
}

func TestCaptureAssistExecutionFeedbackInfersPrimaryArtifactRefs(t *testing.T) {
	t.Parallel()

	feedback := captureAssistExecutionFeedback(
		"john",
		[]string{"--wordlist=/tmp/rockyou.txt", "/tmp/zip.hash"},
		nil,
		[]byte("Loaded 1 password hash"),
		"/tmp/john.log",
	)
	if len(feedback.PrimaryArtifactRefs) != 0 {
		t.Fatalf("expected no primary artifact refs for john argv, got %#v", feedback.PrimaryArtifactRefs)
	}
	if len(feedback.InputPathRefs) != 2 {
		t.Fatalf("expected input path refs to be populated, got %#v", feedback.InputPathRefs)
	}
	if !strings.Contains(strings.Join(feedback.InputPathRefs, ","), "/tmp/zip.hash") {
		t.Fatalf("expected zip hash input ref, got %#v", feedback.InputPathRefs)
	}
}

func TestCaptureAssistExecutionFeedbackInfersOutputArtifactRefs(t *testing.T) {
	t.Parallel()

	feedback := captureAssistExecutionFeedback(
		"nmap",
		[]string{"-sV", "-oX", "/tmp/scan.xml", "192.168.50.1"},
		nil,
		[]byte("Nmap done"),
		"/tmp/nmap.log",
	)
	if len(feedback.PrimaryArtifactRefs) == 0 || feedback.PrimaryArtifactRefs[0] != "/tmp/scan.xml" {
		t.Fatalf("expected primary artifact ref /tmp/scan.xml, got %#v", feedback.PrimaryArtifactRefs)
	}
}

func TestCaptureAssistExecutionFeedbackInfersShellOutputAndInputRefs(t *testing.T) {
	t.Parallel()

	feedback := captureAssistExecutionFeedback(
		"bash",
		[]string{"-c", "ls -la /tmp/secret.zip && zipinfo -v /tmp/secret.zip > /tmp/zip_metadata.txt && unzip -l /tmp/secret.zip | tee -a /tmp/zip_listing.txt"},
		nil,
		[]byte("ok"),
		"/tmp/bash.log",
	)
	if len(feedback.PrimaryArtifactRefs) < 2 {
		t.Fatalf("expected shell output refs, got %#v", feedback.PrimaryArtifactRefs)
	}
	joinedOutputs := strings.Join(feedback.PrimaryArtifactRefs, ",")
	if !strings.Contains(joinedOutputs, "/tmp/zip_metadata.txt") || !strings.Contains(joinedOutputs, "/tmp/zip_listing.txt") {
		t.Fatalf("expected shell output refs in %#v", feedback.PrimaryArtifactRefs)
	}
	joinedInputs := strings.Join(feedback.InputPathRefs, ",")
	if !strings.Contains(joinedInputs, "/tmp/secret.zip") {
		t.Fatalf("expected shell input refs to include secret.zip, got %#v", feedback.InputPathRefs)
	}
	if strings.Contains(joinedInputs, "/tmp/zip_metadata.txt") {
		t.Fatalf("did not expect output ref to remain in input refs, got %#v", feedback.InputPathRefs)
	}
}
