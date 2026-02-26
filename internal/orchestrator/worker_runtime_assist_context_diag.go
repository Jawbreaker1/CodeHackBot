package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type workerAssistPromptEnvelope struct {
	Calls              int `json:"calls"`
	LastRecentLogBytes int `json:"last_recent_log_bytes"`
	LastChatBytes      int `json:"last_chat_bytes"`
	MaxRecentLogBytes  int `json:"max_recent_log_bytes"`
	MaxChatBytes       int `json:"max_chat_bytes"`
	LastRecoverHintLen int `json:"last_recover_hint_len"`
	MaxRecoverHintLen  int `json:"max_recover_hint_len"`
}

type workerAssistObservationEnvelope struct {
	CurrentCount int      `json:"current_count"`
	MaxCount     int      `json:"max_count"`
	Limit        int      `json:"limit"`
	RetainedTail []string `json:"retained_tail,omitempty"`
}

type workerAssistTruncationEnvelope struct {
	OutputByteCapEvents int `json:"output_byte_cap_events"`
	OutputCharCapEvents int `json:"output_char_cap_events"`
	EmptyOutputEvents   int `json:"empty_output_events"`
}

type workerAssistAnchorEnvelope struct {
	LastCommandFingerprint string   `json:"last_command_fingerprint"`
	LastResultFingerprint  string   `json:"last_result_fingerprint"`
	LastFailure            string   `json:"last_failure"`
	RetainedPathAnchors    []string `json:"retained_path_anchors,omitempty"`
}

type workerAssistContextEnvelope struct {
	RunID                string                          `json:"run_id"`
	TaskID               string                          `json:"task_id"`
	WorkerID             string                          `json:"worker_id"`
	Attempt              int                             `json:"attempt"`
	Goal                 string                          `json:"goal"`
	FinalMode            string                          `json:"final_mode"`
	TurnsObserved        int                             `json:"turns_observed"`
	ActionSteps          int                             `json:"action_steps"`
	ToolCalls            int                             `json:"tool_calls"`
	UpdatedAt            string                          `json:"updated_at"`
	CarryoverApplied     bool                            `json:"carryover_applied,omitempty"`
	CarryoverFromAttempt int                             `json:"carryover_from_attempt,omitempty"`
	Prompt               workerAssistPromptEnvelope      `json:"prompt"`
	Observations         workerAssistObservationEnvelope `json:"observations"`
	Truncation           workerAssistTruncationEnvelope  `json:"truncation"`
	Anchors              workerAssistAnchorEnvelope      `json:"anchors"`
	pathAnchorSeen       map[string]struct{}             `json:"-"`
}

func newWorkerAssistContextEnvelope(cfg WorkerRunConfig, task TaskSpec, goal string) *workerAssistContextEnvelope {
	return &workerAssistContextEnvelope{
		RunID:    cfg.RunID,
		TaskID:   cfg.TaskID,
		WorkerID: cfg.WorkerID,
		Attempt:  cfg.Attempt,
		Goal:     strings.TrimSpace(goal),
		Observations: workerAssistObservationEnvelope{
			Limit: workerAssistObsLimit,
		},
		pathAnchorSeen: map[string]struct{}{},
	}
}

func (w *workerAssistContextEnvelope) recordPrompt(mode, recoverHint, recent, chat string) {
	w.FinalMode = strings.TrimSpace(mode)
	w.Prompt.Calls++
	w.Prompt.LastRecentLogBytes = len(recent)
	w.Prompt.LastChatBytes = len(chat)
	if w.Prompt.LastRecentLogBytes > w.Prompt.MaxRecentLogBytes {
		w.Prompt.MaxRecentLogBytes = w.Prompt.LastRecentLogBytes
	}
	if w.Prompt.LastChatBytes > w.Prompt.MaxChatBytes {
		w.Prompt.MaxChatBytes = w.Prompt.LastChatBytes
	}
	w.Prompt.LastRecoverHintLen = len(strings.TrimSpace(recoverHint))
	if w.Prompt.LastRecoverHintLen > w.Prompt.MaxRecoverHintLen {
		w.Prompt.MaxRecoverHintLen = w.Prompt.LastRecoverHintLen
	}
}

func (w *workerAssistContextEnvelope) recordProgress(turn, actionSteps, toolCalls, observationCount int) {
	if turn > w.TurnsObserved {
		w.TurnsObserved = turn
	}
	w.ActionSteps = actionSteps
	w.ToolCalls = toolCalls
	w.Observations.CurrentCount = observationCount
	if observationCount > w.Observations.MaxCount {
		w.Observations.MaxCount = observationCount
	}
}

func (w *workerAssistContextEnvelope) recordObservationSnapshot(observations []string) {
	if len(observations) == 0 {
		w.Observations.RetainedTail = nil
		return
	}
	if len(observations) > workerAssistObsLimit {
		observations = observations[len(observations)-workerAssistObsLimit:]
	}
	w.Observations.RetainedTail = append([]string{}, observations...)
}

func (w *workerAssistContextEnvelope) recordCommandSummary(meta workerAssistCommandSummaryMeta) {
	if meta.OutputByteCapped {
		w.Truncation.OutputByteCapEvents++
	}
	if meta.OutputCharCapped {
		w.Truncation.OutputCharCapEvents++
	}
	if meta.OutputWasEmptyText {
		w.Truncation.EmptyOutputEvents++
	}
}

func (w *workerAssistContextEnvelope) recordCommandOutcome(command string, args []string, runErr error, resultKey string) {
	w.Anchors.LastCommandFingerprint = strings.TrimSpace(strings.Join(append([]string{strings.TrimSpace(command)}, args...), " "))
	w.Anchors.LastResultFingerprint = strings.TrimSpace(resultKey)
	if runErr != nil {
		w.Anchors.LastFailure = strings.TrimSpace(runErr.Error())
	}
	w.recordPathAnchors(args)
}

func (w *workerAssistContextEnvelope) recordPathAnchors(args []string) {
	for _, raw := range args {
		for _, token := range splitAnchorTokens(raw) {
			anchor := normalizePathAnchorToken(token)
			if anchor == "" {
				continue
			}
			if _, exists := w.pathAnchorSeen[anchor]; exists {
				continue
			}
			w.pathAnchorSeen[anchor] = struct{}{}
			w.Anchors.RetainedPathAnchors = append(w.Anchors.RetainedPathAnchors, anchor)
			if len(w.Anchors.RetainedPathAnchors) > 32 {
				drop := w.Anchors.RetainedPathAnchors[0]
				delete(w.pathAnchorSeen, drop)
				w.Anchors.RetainedPathAnchors = append([]string{}, w.Anchors.RetainedPathAnchors[1:]...)
			}
		}
	}
}

func (w *workerAssistContextEnvelope) applyCarryover(prior *workerAssistContextEnvelope) {
	if prior == nil {
		return
	}
	w.CarryoverApplied = true
	w.CarryoverFromAttempt = prior.Attempt
	for _, anchor := range prior.Anchors.RetainedPathAnchors {
		if _, exists := w.pathAnchorSeen[anchor]; exists {
			continue
		}
		w.pathAnchorSeen[anchor] = struct{}{}
		w.Anchors.RetainedPathAnchors = append(w.Anchors.RetainedPathAnchors, anchor)
	}
}

func writeWorkerAssistContextEnvelope(cfg WorkerRunConfig, taskID string, envelope *workerAssistContextEnvelope, now func() time.Time) error {
	if envelope == nil {
		return nil
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	envelope.UpdatedAt = now().UTC().Format(time.RFC3339Nano)
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, taskID)
	attemptPath := filepath.Join(artifactDir, fmt.Sprintf("context_envelope.a%d.json", maxInt(1, cfg.Attempt)))
	if err := WriteJSONAtomic(attemptPath, envelope); err != nil {
		return err
	}
	return WriteJSONAtomic(filepath.Join(artifactDir, "context_envelope.json"), envelope)
}

func loadPreviousWorkerAssistContextEnvelope(cfg WorkerRunConfig, taskID string) (*workerAssistContextEnvelope, error) {
	if cfg.Attempt <= 1 {
		return nil, nil
	}
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, taskID)
	candidates := []string{
		filepath.Join(artifactDir, fmt.Sprintf("context_envelope.a%d.json", cfg.Attempt-1)),
		filepath.Join(artifactDir, "context_envelope.json"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var envelope workerAssistContextEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, err
		}
		return &envelope, nil
	}
	return nil, nil
}

func splitAnchorTokens(raw string) []string {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func normalizePathAnchorToken(token string) string {
	trimmed := strings.TrimSpace(token)
	trimmed = strings.Trim(trimmed, "\"'()[]{}<>.,;:")
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "-") {
		return ""
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, `\`) {
		return filepath.Clean(trimmed)
	}
	ext := strings.ToLower(filepath.Ext(trimmed))
	switch ext {
	case ".zip", ".hash", ".txt", ".md", ".json", ".log", ".xml", ".csv", ".html", ".sh", ".py":
		return filepath.Clean(trimmed)
	default:
		return ""
	}
}
