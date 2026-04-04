package contextinspect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
	"github.com/Jawbreaker1/CodeHackBot/internal/workeraction"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerstep"
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

func TestRecorderCapturePlannerAttempt(t *testing.T) {
	dir := t.TempDir()
	recorder := Recorder{Dir: dir}
	attempt := workerplan.AttemptRecord{
		Prompt:      "planner prompt",
		RawResponse: `{"mode":"planned_execution"}`,
		Parsed: workerplan.Plan{
			Mode:        workerplan.ModePlannedExecution,
			WorkerGoal:  "inspect zip",
			PlanSummary: "Inspect then recover.",
			PlanSteps:   []string{"Inspect archive", "Attempt recovery"},
			ActiveStep:  "Inspect archive",
		},
		Accepted:   true,
		FinalError: "",
	}
	if err := recorder.CapturePlannerAttempt(attempt); err != nil {
		t.Fatalf("CapturePlannerAttempt() error = %v", err)
	}
	path := filepath.Join(dir, "planner-attempt-001.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"[planner_attempt]",
		"accepted: true",
		"[plan]",
		"mode: planned_execution",
		"plan_steps: Inspect archive | Attempt recovery",
		"[validation]",
		"[prompt]",
		"planner prompt",
		"[raw_response]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("planner attempt missing %q in:\n%s", want, text)
		}
	}
}

func TestRecorderCaptureActionReviewAttempt(t *testing.T) {
	dir := t.TempDir()
	recorder := Recorder{Dir: dir}
	attempt := workeraction.AttemptRecord{
		Prompt:      "action review prompt",
		RawResponse: `{"decision":"revise","reason":"too broad for active step"}`,
		Parsed: workeraction.Review{
			Decision: workeraction.DecisionRevise,
			Reason:   "too broad for active step",
		},
		Accepted: true,
	}
	if err := recorder.CaptureActionReviewAttempt(attempt); err != nil {
		t.Fatalf("CaptureActionReviewAttempt() error = %v", err)
	}
	path := filepath.Join(dir, "action-review-attempt-001.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"[action_review_attempt]",
		"accepted: true",
		"[review]",
		"decision: revise",
		"reason: too broad for active step",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("action review attempt missing %q in:\n%s", want, text)
		}
	}
}

func TestRecorderCaptureStepEvaluationAttempt(t *testing.T) {
	dir := t.TempDir()
	recorder := Recorder{Dir: dir}
	attempt := workerstep.AttemptRecord{
		Prompt:      "step evaluation prompt",
		RawResponse: `{"status":"satisfied","reason":"metadata collected","summary":"step satisfied"}`,
		Parsed: workerstep.Evaluation{
			Status:  workerstep.StatusSatisfied,
			Reason:  "metadata collected",
			Summary: "step satisfied",
		},
		Accepted: true,
	}
	if err := recorder.CaptureStepEvaluationAttempt(attempt); err != nil {
		t.Fatalf("CaptureStepEvaluationAttempt() error = %v", err)
	}
	path := filepath.Join(dir, "step-eval-attempt-001.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"[step_evaluation_attempt]",
		"accepted: true",
		"[evaluation]",
		"status: satisfied",
		"reason: metadata collected",
		"[prompt]",
		"step evaluation prompt",
		"[raw_response]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("step evaluation attempt missing %q in:\n%s", want, text)
		}
	}
}
