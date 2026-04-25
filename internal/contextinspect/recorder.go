package contextinspect

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextstats"
	"github.com/Jawbreaker1/CodeHackBot/internal/workeraction"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerdirect"
	"github.com/Jawbreaker1/CodeHackBot/internal/workermode"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerstep"
	"github.com/Jawbreaker1/CodeHackBot/internal/workertask"
)

// Recorder writes human-readable context packet snapshots for live diagnosis.
type Recorder struct {
	Dir string
}

// Capture writes one context snapshot for a given step and stage.
func (r Recorder) Capture(step int, stage string, packet ctxpacket.WorkerPacket) error {
	if step <= 0 {
		return fmt.Errorf("step must be positive")
	}
	if stage == "" {
		return fmt.Errorf("stage is required")
	}
	if r.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("step-%03d-%s.txt", step, stage))
	rendered := packet.Render() + "\n"
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	metaPath := filepath.Join(r.Dir, fmt.Sprintf("step-%03d-%s-meta.txt", step, stage))
	if err := os.WriteFile(metaPath, []byte(renderMeta(path, rendered, packet.RenderSections())), 0o644); err != nil {
		return fmt.Errorf("write snapshot metadata: %w", err)
	}
	validationPath := filepath.Join(r.Dir, fmt.Sprintf("step-%03d-%s-validation.txt", step, stage))
	if err := os.WriteFile(validationPath, []byte(renderValidation(validationPath, ctxpacket.ValidatePacket(packet))), 0o644); err != nil {
		return fmt.Errorf("write snapshot validation: %w", err)
	}
	return nil
}

func (r Recorder) CapturePlannerAttempt(attempt workerplan.AttemptRecord) error {
	if r.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("planner-attempt-%03d.txt", nextPlannerAttemptIndex(r.Dir)))
	if err := os.WriteFile(path, []byte(renderPlannerAttempt(path, attempt)), 0o644); err != nil {
		return fmt.Errorf("write planner attempt: %w", err)
	}
	return nil
}

func (r Recorder) CaptureActionReviewAttempt(attempt workeraction.AttemptRecord) error {
	if r.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("action-review-attempt-%03d.txt", nextActionReviewAttemptIndex(r.Dir)))
	if err := os.WriteFile(path, []byte(renderActionReviewAttempt(path, attempt)), 0o644); err != nil {
		return fmt.Errorf("write action review attempt: %w", err)
	}
	return nil
}

func (r Recorder) CaptureDirectEvaluationAttempt(attempt workerdirect.AttemptRecord) error {
	if r.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("direct-eval-attempt-%03d.txt", nextDirectEvalAttemptIndex(r.Dir)))
	if err := os.WriteFile(path, []byte(renderDirectEvaluationAttempt(path, attempt)), 0o644); err != nil {
		return fmt.Errorf("write direct evaluation attempt: %w", err)
	}
	return nil
}

func (r Recorder) CaptureClassificationAttempt(attempt workermode.AttemptRecord) error {
	if r.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("classification-attempt-%03d.txt", nextClassificationAttemptIndex(r.Dir)))
	if err := os.WriteFile(path, []byte(renderClassificationAttempt(path, attempt)), 0o644); err != nil {
		return fmt.Errorf("write classification attempt: %w", err)
	}
	return nil
}

func (r Recorder) CaptureTaskBoundaryAttempt(attempt workertask.AttemptRecord) error {
	if r.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("task-boundary-attempt-%03d.txt", nextTaskBoundaryAttemptIndex(r.Dir)))
	if err := os.WriteFile(path, []byte(renderTaskBoundaryAttempt(path, attempt)), 0o644); err != nil {
		return fmt.Errorf("write task boundary attempt: %w", err)
	}
	return nil
}

func (r Recorder) CaptureStepEvaluationAttempt(attempt workerstep.AttemptRecord) error {
	if r.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("step-eval-attempt-%03d.txt", nextStepEvalAttemptIndex(r.Dir)))
	if err := os.WriteFile(path, []byte(renderStepEvaluationAttempt(path, attempt)), 0o644); err != nil {
		return fmt.Errorf("write step evaluation attempt: %w", err)
	}
	return nil
}

func renderTaskBoundaryAttempt(path string, attempt workertask.AttemptRecord) string {
	lines := []string{
		"[task_boundary_attempt]",
		"path: " + path,
		fmt.Sprintf("accepted: %t", attempt.Accepted),
		"final_error: " + blankOrNone(attempt.FinalError),
		"",
		"[decision]",
		"action: " + blankOrNone(string(attempt.Parsed.Action)),
		"reason: " + blankOrNone(attempt.Parsed.Reason),
		"",
		"[validation]",
		fmt.Sprintf("issue_count: %d", len(attempt.Validation.Issues)),
	}
	if len(attempt.Validation.Issues) == 0 {
		lines = append(lines, "(none)")
	} else {
		for i, issue := range attempt.Validation.Issues {
			lines = append(lines, fmt.Sprintf("%d. message=%s", i+1, issue.Message))
		}
	}
	lines = append(lines,
		"",
		"[prompt]",
		blankOrNone(attempt.Prompt),
		"",
		"[raw_response]",
		blankOrNone(attempt.RawResponse),
	)
	return strings.Join(lines, "\n") + "\n"
}

func renderClassificationAttempt(path string, attempt workermode.AttemptRecord) string {
	lines := []string{
		"[classification_attempt]",
		"path: " + path,
		fmt.Sprintf("accepted: %t", attempt.Accepted),
		"final_error: " + blankOrNone(attempt.FinalError),
		"",
		"[decision]",
		"mode: " + blankOrNone(string(attempt.Parsed.Mode)),
		"reason: " + blankOrNone(attempt.Parsed.Reason),
		"",
		"[validation]",
		fmt.Sprintf("issue_count: %d", len(attempt.Validation.Issues)),
	}
	if len(attempt.Validation.Issues) == 0 {
		lines = append(lines, "(none)")
	} else {
		for i, issue := range attempt.Validation.Issues {
			lines = append(lines, fmt.Sprintf("%d. message=%s", i+1, issue.Message))
		}
	}
	lines = append(lines,
		"",
		"[prompt]",
		blankOrNone(attempt.Prompt),
		"",
		"[raw_response]",
		blankOrNone(attempt.RawResponse),
	)
	return strings.Join(lines, "\n") + "\n"
}

func renderDirectEvaluationAttempt(path string, attempt workerdirect.AttemptRecord) string {
	lines := []string{
		"[direct_evaluation_attempt]",
		"path: " + path,
		fmt.Sprintf("accepted: %t", attempt.Accepted),
		"final_error: " + blankOrNone(attempt.FinalError),
		"",
		"[evaluation]",
		"status: " + blankOrNone(string(attempt.Parsed.Status)),
		"reason: " + blankOrNone(attempt.Parsed.Reason),
		"summary: " + blankOrNone(attempt.Parsed.Summary),
		"",
		"[validation]",
		fmt.Sprintf("issue_count: %d", len(attempt.Validation.Issues)),
	}
	if len(attempt.Validation.Issues) == 0 {
		lines = append(lines, "(none)")
	} else {
		for i, issue := range attempt.Validation.Issues {
			lines = append(lines, fmt.Sprintf("%d. message=%s", i+1, issue.Message))
		}
	}
	lines = append(lines,
		"",
		"[prompt]",
		blankOrNone(attempt.Prompt),
		"",
		"[raw_response]",
		blankOrNone(attempt.RawResponse),
	)
	return strings.Join(lines, "\n") + "\n"
}

func renderMeta(snapshotPath, rendered string, sections []ctxpacket.RenderedSection) string {
	stats := contextstats.Build(rendered, sections)
	lines := []string{
		"[snapshot]",
		"path: " + snapshotPath,
		fmt.Sprintf("total_chars: %d", stats.TotalChars),
		fmt.Sprintf("total_lines: %d", stats.TotalLines),
		fmt.Sprintf("approx_total_tokens: %d", stats.ApproxTotalTokens),
		fmt.Sprintf("section_count: %d", stats.SectionCount),
		"",
		"[sections]",
	}
	for _, section := range stats.Sections {
		lines = append(lines,
			fmt.Sprintf("%s: chars=%d lines=%d approx_tokens=%d", section.Name, section.Chars, section.Lines, section.ApproxTokens),
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

func renderValidation(path string, report ctxpacket.ValidationReport) string {
	lines := []string{
		"[packet_validation]",
		"path: " + path,
		"summary: " + report.Summary(),
		"highest_severity: " + string(report.HighestSeverity()),
		fmt.Sprintf("issue_count: %d", len(report.Issues)),
		"",
		"[issues]",
	}
	if len(report.Issues) == 0 {
		lines = append(lines, "(none)")
	} else {
		for i, issue := range report.Issues {
			lines = append(lines, fmt.Sprintf("%d. severity=%s code=%s message=%s", i+1, issue.Severity, issue.Code, issue.Message))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func renderPlannerAttempt(path string, attempt workerplan.AttemptRecord) string {
	lines := []string{
		"[planner_attempt]",
		"path: " + path,
		fmt.Sprintf("accepted: %t", attempt.Accepted),
		"final_error: " + blankOrNone(attempt.FinalError),
		"",
		"[plan]",
		"mode: " + blankOrNone(string(attempt.Parsed.Mode)),
		"worker_goal: " + blankOrNone(attempt.Parsed.WorkerGoal),
		"plan_summary: " + blankOrNone(attempt.Parsed.PlanSummary),
		"plan_steps: " + renderItems(attempt.Parsed.PlanSteps),
		"active_step: " + blankOrNone(attempt.Parsed.ActiveStep),
		"replan_conditions: " + renderItems(attempt.Parsed.ReplanConditions),
		"",
		"[validation]",
		fmt.Sprintf("issue_count: %d", len(attempt.Validation.Issues)),
	}
	if len(attempt.Validation.Issues) == 0 {
		lines = append(lines, "(none)")
	} else {
		for i, issue := range attempt.Validation.Issues {
			lines = append(lines, fmt.Sprintf("%d. message=%s", i+1, issue.Message))
		}
	}
	lines = append(lines,
		"",
		"[prompt]",
		blankOrNone(attempt.Prompt),
		"",
		"[raw_response]",
		blankOrNone(attempt.RawResponse),
	)
	return strings.Join(lines, "\n") + "\n"
}

func renderActionReviewAttempt(path string, attempt workeraction.AttemptRecord) string {
	lines := []string{
		"[action_review_attempt]",
		"path: " + path,
		fmt.Sprintf("accepted: %t", attempt.Accepted),
		"final_error: " + blankOrNone(attempt.FinalError),
		"",
		"[review]",
		"decision: " + blankOrNone(string(attempt.Parsed.Decision)),
		"reason: " + blankOrNone(attempt.Parsed.Reason),
		"",
		"[validation]",
		fmt.Sprintf("issue_count: %d", len(attempt.Validation.Issues)),
	}
	if len(attempt.Validation.Issues) == 0 {
		lines = append(lines, "(none)")
	} else {
		for i, issue := range attempt.Validation.Issues {
			lines = append(lines, fmt.Sprintf("%d. message=%s", i+1, issue.Message))
		}
	}
	lines = append(lines,
		"",
		"[prompt]",
		blankOrNone(attempt.Prompt),
		"",
		"[raw_response]",
		blankOrNone(attempt.RawResponse),
	)
	return strings.Join(lines, "\n") + "\n"
}

func renderStepEvaluationAttempt(path string, attempt workerstep.AttemptRecord) string {
	lines := []string{
		"[step_evaluation_attempt]",
		"path: " + path,
		fmt.Sprintf("accepted: %t", attempt.Accepted),
		"final_error: " + blankOrNone(attempt.FinalError),
		"",
		"[evaluation]",
		"status: " + blankOrNone(string(attempt.Parsed.Status)),
		"reason: " + blankOrNone(attempt.Parsed.Reason),
		"summary: " + blankOrNone(attempt.Parsed.Summary),
		"",
		"[validation]",
		fmt.Sprintf("issue_count: %d", len(attempt.Validation.Issues)),
	}
	if len(attempt.Validation.Issues) == 0 {
		lines = append(lines, "(none)")
	} else {
		for i, issue := range attempt.Validation.Issues {
			lines = append(lines, fmt.Sprintf("%d. message=%s", i+1, issue.Message))
		}
	}
	lines = append(lines,
		"",
		"[prompt]",
		blankOrNone(attempt.Prompt),
		"",
		"[raw_response]",
		blankOrNone(attempt.RawResponse),
	)
	return strings.Join(lines, "\n") + "\n"
}

func nextPlannerAttemptIndex(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "planner-attempt-*.txt"))
	if err != nil || len(matches) == 0 {
		return 1
	}
	sort.Strings(matches)
	return len(matches) + 1
}

func nextActionReviewAttemptIndex(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "action-review-attempt-*.txt"))
	if err != nil || len(matches) == 0 {
		return 1
	}
	sort.Strings(matches)
	return len(matches) + 1
}

func nextStepEvalAttemptIndex(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "step-eval-attempt-*.txt"))
	if err != nil || len(matches) == 0 {
		return 1
	}
	sort.Strings(matches)
	return len(matches) + 1
}

func nextDirectEvalAttemptIndex(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "direct-eval-attempt-*.txt"))
	if err != nil || len(matches) == 0 {
		return 1
	}
	sort.Strings(matches)
	return len(matches) + 1
}

func nextClassificationAttemptIndex(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "classification-attempt-*.txt"))
	if err != nil || len(matches) == 0 {
		return 1
	}
	sort.Strings(matches)
	return len(matches) + 1
}

func nextTaskBoundaryAttemptIndex(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "task-boundary-attempt-*.txt"))
	if err != nil || len(matches) == 0 {
		return 1
	}
	sort.Strings(matches)
	return len(matches) + 1
}

func renderItems(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, " | ")
}

func blankOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
