package contextinspect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/contextstats"
	"github.com/Jawbreaker1/CodeHackBot/internal/workergoal"
	"github.com/Jawbreaker1/CodeHackBot/internal/workermode"
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
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("step-%03d-%s.txt", step, stage))
	rendered := packet.Render() + "\n"
	if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	if stage == "pre-llm" {
		sections, err := json.MarshalIndent(packet.RenderSections(), "", "  ")
		if err != nil {
			return fmt.Errorf("encode context sections: %w", err)
		}
		if err := os.WriteFile(filepath.Join(r.Dir, fmt.Sprintf("step-%03d-pre-llm-sections.json", step)), sections, 0o600); err != nil {
			return fmt.Errorf("write context sections: %w", err)
		}
	}
	metaPath := filepath.Join(r.Dir, fmt.Sprintf("step-%03d-%s-meta.txt", step, stage))
	if err := os.WriteFile(metaPath, []byte(renderMeta(path, rendered, packet.RenderSections())), 0o600); err != nil {
		return fmt.Errorf("write snapshot metadata: %w", err)
	}
	validationPath := filepath.Join(r.Dir, fmt.Sprintf("step-%03d-%s-validation.txt", step, stage))
	if err := os.WriteFile(validationPath, []byte(renderValidation(validationPath, ctxpacket.ValidatePacket(packet))), 0o600); err != nil {
		return fmt.Errorf("write snapshot validation: %w", err)
	}
	return nil
}

// CaptureModelRequest records the exact ordered messages sent to the worker
// model; the human-readable packet alone omits the worker decision contract.
func (r Recorder) CaptureModelRequest(step int, messages any) error {
	if step <= 0 || r.Dir == "" {
		return fmt.Errorf("step and context directory are required")
	}
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.Dir, fmt.Sprintf("step-%03d-request.json", step)), data, 0o600)
}

// ReadOmissions is a debug-only projection override. The underlying packet,
// evidence and historical snapshots are never edited.
func (r Recorder) ReadOmissions() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(r.Dir, "debug-omissions.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var value struct {
		Sections []string `json:"sections"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("read debug omissions: %w", err)
	}
	return value.Sections, nil
}

func (r Recorder) CaptureGoalEvaluationAttempt(attempt workergoal.AttemptRecord) error {
	if r.Dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir inspect dir: %w", err)
	}
	path := filepath.Join(r.Dir, fmt.Sprintf("goal-eval-attempt-%03d.txt", nextGoalEvalAttemptIndex(r.Dir)))
	if err := os.WriteFile(path, []byte(renderGoalEvaluationAttempt(path, attempt)), 0o644); err != nil {
		return fmt.Errorf("write goal evaluation attempt: %w", err)
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

func renderTaskBoundaryAttempt(path string, attempt workertask.AttemptRecord) string {
	lines := []string{
		"[task_boundary_attempt]",
		"path: " + path,
		fmt.Sprintf("accepted: %t", attempt.Accepted),
		"final_error: " + blankOrNone(attempt.FinalError),
		"response_source: " + blankOrNone(attempt.ResponseSource),
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
		"response_source: " + blankOrNone(attempt.ResponseSource),
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

func renderGoalEvaluationAttempt(path string, attempt workergoal.AttemptRecord) string {
	lines := []string{
		"[goal_evaluation_attempt]",
		"path: " + path,
		fmt.Sprintf("accepted: %t", attempt.Accepted),
		"final_error: " + blankOrNone(attempt.FinalError),
		"response_source: " + blankOrNone(attempt.ResponseSource),
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

func nextGoalEvalAttemptIndex(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "goal-eval-attempt-*.txt"))
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
