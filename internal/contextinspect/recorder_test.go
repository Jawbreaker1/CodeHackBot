package contextinspect

import (
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/workergoal"
	"github.com/Jawbreaker1/CodeHackBot/internal/workermode"
	"github.com/Jawbreaker1/CodeHackBot/internal/workertask"
)

func TestRecorderCapture(t *testing.T) {
	dir := t.TempDir()
	recorder := Recorder{Dir: dir}
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "test goal", ReportingRequirement: "owasp"},
		RunningSummary:    "summary",
	}
	if err := recorder.Capture(1, "pre-llm", packet); err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	path := filepath.Join(dir, "step-001-pre-llm.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "[session_foundation]") || !strings.Contains(text, "test goal") {
		t.Fatalf("snapshot missing expected content:\n%s", text)
	}

	metaPath := filepath.Join(dir, "step-001-pre-llm-meta.txt")
	meta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("ReadFile(meta) error = %v", err)
	}
	metaText := string(meta)
	for _, want := range []string{
		"[snapshot]",
		"total_chars:",
		"approx_total_tokens:",
		"section_count:",
		"[sections]",
		"behavior_frame: chars=",
		"approx_tokens=",
		"session_foundation: chars=",
	} {
		if !strings.Contains(metaText, want) {
			t.Fatalf("meta snapshot missing %q in:\n%s", want, metaText)
		}
	}

	validationPath := filepath.Join(dir, "step-001-pre-llm-validation.txt")
	validation, err := os.ReadFile(validationPath)
	if err != nil {
		t.Fatalf("ReadFile(validation) error = %v", err)
	}
	validationText := string(validation)
	for _, want := range []string{
		"[packet_validation]",
		"summary:",
		"highest_severity:",
		"issue_count:",
		"[issues]",
	} {
		if !strings.Contains(validationText, want) {
			t.Fatalf("validation snapshot missing %q in:\n%s", want, validationText)
		}
	}
}

func TestRecorderCaptureGoalEvaluationAttempt(t *testing.T) {
	dir := t.TempDir()
	recorder := Recorder{Dir: dir}
	attempt := workergoal.AttemptRecord{
		Prompt:      "direct prompt",
		RawResponse: `{"status":"satisfied","reason":"file listing is complete","summary":"listed files"}`,
		Parsed: workergoal.Evaluation{
			Status:  workergoal.StatusSatisfied,
			Reason:  "file listing is complete",
			Summary: "listed files",
		},
		Accepted: true,
	}
	if err := recorder.CaptureGoalEvaluationAttempt(attempt); err != nil {
		t.Fatalf("CaptureGoalEvaluationAttempt() error = %v", err)
	}
	path := filepath.Join(dir, "goal-eval-attempt-001.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(goal evaluation attempt) error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"[goal_evaluation_attempt]",
		"accepted: true",
		"status: satisfied",
		"reason: file listing is complete",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("goal evaluation attempt missing %q in:\n%s", want, text)
		}
	}
}

func TestRecorderCaptureTaskBoundaryAttempt(t *testing.T) {
	dir := t.TempDir()
	recorder := Recorder{Dir: dir}
	attempt := workertask.AttemptRecord{
		Prompt:      "boundary prompt",
		RawResponse: `{"action":"start_new_task","reason":"latest turn introduces a new goal"}`,
		Parsed: workertask.Decision{
			Action: workertask.ActionStartNewTask,
			Reason: "latest turn introduces a new goal",
		},
		Accepted: true,
	}
	if err := recorder.CaptureTaskBoundaryAttempt(attempt); err != nil {
		t.Fatalf("CaptureTaskBoundaryAttempt() error = %v", err)
	}
	path := filepath.Join(dir, "task-boundary-attempt-001.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(task boundary attempt) error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"[task_boundary_attempt]",
		"accepted: true",
		"action: start_new_task",
		"reason: latest turn introduces a new goal",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("task boundary attempt missing %q in:\n%s", want, text)
		}
	}
}

func TestRecorderCaptureClassificationAttempt(t *testing.T) {
	dir := t.TempDir()
	recorder := Recorder{Dir: dir}
	attempt := workermode.AttemptRecord{
		Prompt:      "classification prompt",
		RawResponse: `{"mode":"conversation","reason":"identity question"}`,
		Parsed: workermode.Decision{
			Mode:   workerplan.ModeConversation,
			Reason: "identity question",
		},
		Accepted: true,
	}
	if err := recorder.CaptureClassificationAttempt(attempt); err != nil {
		t.Fatalf("CaptureClassificationAttempt() error = %v", err)
	}
	path := filepath.Join(dir, "classification-attempt-001.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(classification attempt) error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"[classification_attempt]",
		"accepted: true",
		"mode: conversation",
		"reason: identity question",
		"classification prompt",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("classification attempt missing %q in:\n%s", want, text)
		}
	}
}
