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
	CurrentCount                int      `json:"current_count"`
	MaxCount                    int      `json:"max_count"`
	Limit                       int      `json:"limit"`
	AppendCalls                 int      `json:"append_calls"`
	EvictionEvents              int      `json:"eviction_events"`
	TokenBudgetCompactionEvents int      `json:"token_budget_compaction_events"`
	CompactionSummaryEvents     int      `json:"compaction_summary_events"`
	RetainedTail                []string `json:"retained_tail,omitempty"`
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

type workerAssistFingerprintEnvelope struct {
	LastActionFingerprint string `json:"last_action_fingerprint,omitempty"`
	LastActionStreak      int    `json:"last_action_streak,omitempty"`
	ActionRepeatEvents    int    `json:"action_repeat_events"`
	LastResultFingerprint string `json:"last_result_fingerprint,omitempty"`
	LastResultStreak      int    `json:"last_result_streak,omitempty"`
	ResultRepeatEvents    int    `json:"result_repeat_events"`
}

type workerAssistRetryEnvelope struct {
	PriorAttempt                  int      `json:"prior_attempt,omitempty"`
	PriorObservationCount         int      `json:"prior_observation_count,omitempty"`
	PriorPathAnchorCount          int      `json:"prior_path_anchor_count,omitempty"`
	CarryoverObservationCount     int      `json:"carryover_observation_count,omitempty"`
	CarryoverPathAnchorCount      int      `json:"carryover_path_anchor_count,omitempty"`
	RestoredLastFailure           bool     `json:"restored_last_failure,omitempty"`
	RestoredLastResultFingerprint bool     `json:"restored_last_result_fingerprint,omitempty"`
	ResetSignals                  []string `json:"reset_signals,omitempty"`
}

type workerAssistAttemptDeltaEnvelope struct {
	ComparedToAttempt int      `json:"compared_to_attempt,omitempty"`
	Summary           []string `json:"summary,omitempty"`
}

type workerAssistContextEnvelope struct {
	RunID                string                           `json:"run_id"`
	TaskID               string                           `json:"task_id"`
	WorkerID             string                           `json:"worker_id"`
	Attempt              int                              `json:"attempt"`
	Goal                 string                           `json:"goal"`
	FinalMode            string                           `json:"final_mode"`
	TurnsObserved        int                              `json:"turns_observed"`
	ActionSteps          int                              `json:"action_steps"`
	ToolCalls            int                              `json:"tool_calls"`
	UpdatedAt            string                           `json:"updated_at"`
	CarryoverApplied     bool                             `json:"carryover_applied,omitempty"`
	CarryoverFromAttempt int                              `json:"carryover_from_attempt,omitempty"`
	Prompt               workerAssistPromptEnvelope       `json:"prompt"`
	Observations         workerAssistObservationEnvelope  `json:"observations"`
	Truncation           workerAssistTruncationEnvelope   `json:"truncation"`
	Anchors              workerAssistAnchorEnvelope       `json:"anchors"`
	Fingerprints         workerAssistFingerprintEnvelope  `json:"fingerprints"`
	Retry                workerAssistRetryEnvelope        `json:"retry,omitempty"`
	AttemptDelta         workerAssistAttemptDeltaEnvelope `json:"attempt_delta,omitempty"`
	pathAnchorSeen       map[string]struct{}              `json:"-"`
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

func (w *workerAssistContextEnvelope) recordObservationAppend(before, after []string) {
	w.Observations.AppendCalls++
	if len(after) < len(before)+1 {
		w.Observations.EvictionEvents++
	}
	if observationTokens(after) <= observationTokens(before) && len(before) > 0 {
		w.Observations.TokenBudgetCompactionEvents++
	}
	if !hasObservationCompactionSummary(before) && hasObservationCompactionSummary(after) {
		w.Observations.CompactionSummaryEvents++
	}
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

func (w *workerAssistContextEnvelope) recordActionFingerprint(actionKey string, streak int) {
	actionKey = strings.TrimSpace(actionKey)
	if actionKey == "" {
		return
	}
	w.Fingerprints.LastActionFingerprint = actionKey
	w.Fingerprints.LastActionStreak = maxInt(1, streak)
	if streak > 1 {
		w.Fingerprints.ActionRepeatEvents++
	}
}

func (w *workerAssistContextEnvelope) recordResultFingerprint(resultKey string, streak int) {
	resultKey = strings.TrimSpace(resultKey)
	if resultKey == "" {
		return
	}
	w.Fingerprints.LastResultFingerprint = resultKey
	w.Fingerprints.LastResultStreak = maxInt(1, streak)
	if streak > 1 {
		w.Fingerprints.ResultRepeatEvents++
	}
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
	w.Retry.PriorAttempt = prior.Attempt
	w.Retry.PriorObservationCount = prior.Observations.CurrentCount
	if w.Retry.PriorObservationCount == 0 {
		w.Retry.PriorObservationCount = len(prior.Observations.RetainedTail)
	}
	w.Retry.PriorPathAnchorCount = len(prior.Anchors.RetainedPathAnchors)
	for _, anchor := range prior.Anchors.RetainedPathAnchors {
		if _, exists := w.pathAnchorSeen[anchor]; exists {
			continue
		}
		w.pathAnchorSeen[anchor] = struct{}{}
		w.Anchors.RetainedPathAnchors = append(w.Anchors.RetainedPathAnchors, anchor)
	}
	w.Retry.CarryoverPathAnchorCount = len(w.Anchors.RetainedPathAnchors)
}

func (w *workerAssistContextEnvelope) recordCarryoverObservations(count int) {
	if count > 0 {
		w.Retry.CarryoverObservationCount = count
	}
}

func (w *workerAssistContextEnvelope) recordCarryoverRecoverySignals(restoredFailure, restoredResultFingerprint bool) {
	if restoredFailure {
		w.Retry.RestoredLastFailure = true
	}
	if restoredResultFingerprint {
		w.Retry.RestoredLastResultFingerprint = true
	}
}

func (w *workerAssistContextEnvelope) finalizeRetryResetSignals(prior *workerAssistContextEnvelope) {
	if prior == nil {
		return
	}
	signals := make([]string, 0, 3)
	if w.Retry.PriorObservationCount > 0 && w.Retry.CarryoverObservationCount == 0 {
		signals = append(signals, "carryover_observations_missing")
	}
	if strings.TrimSpace(prior.Anchors.LastFailure) != "" && !w.Retry.RestoredLastFailure {
		signals = append(signals, "last_failure_not_restored")
	}
	if strings.TrimSpace(prior.Anchors.LastResultFingerprint) != "" && !w.Retry.RestoredLastResultFingerprint {
		signals = append(signals, "last_result_fingerprint_not_restored")
	}
	if len(signals) > 0 {
		w.Retry.ResetSignals = append([]string{}, signals...)
	}
}

func (w *workerAssistContextEnvelope) finalizeAttemptDelta(prior *workerAssistContextEnvelope) string {
	if prior == nil {
		return ""
	}
	summary := []string{
		fmt.Sprintf("observations=%d->%d", prior.Observations.CurrentCount, w.Observations.CurrentCount),
		fmt.Sprintf("anchors=%d->%d", len(prior.Anchors.RetainedPathAnchors), len(w.Anchors.RetainedPathAnchors)),
		fmt.Sprintf("truncation_delta(byte=%+d,char=%+d,empty=%+d)",
			w.Truncation.OutputByteCapEvents-prior.Truncation.OutputByteCapEvents,
			w.Truncation.OutputCharCapEvents-prior.Truncation.OutputCharCapEvents,
			w.Truncation.EmptyOutputEvents-prior.Truncation.EmptyOutputEvents),
		fmt.Sprintf("repeat_events_delta(action=%+d,result=%+d)",
			w.Fingerprints.ActionRepeatEvents-prior.Fingerprints.ActionRepeatEvents,
			w.Fingerprints.ResultRepeatEvents-prior.Fingerprints.ResultRepeatEvents),
	}
	if len(w.Retry.ResetSignals) > 0 {
		summary = append(summary, "reset_signals="+strings.Join(w.Retry.ResetSignals, ","))
	}
	w.AttemptDelta = workerAssistAttemptDeltaEnvelope{
		ComparedToAttempt: prior.Attempt,
		Summary:           append([]string{}, summary...),
	}
	return strings.Join(summary, " | ")
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

func hasObservationCompactionSummary(observations []string) bool {
	for _, entry := range observations {
		trimmed := strings.TrimSpace(entry)
		if strings.HasPrefix(trimmed, "compaction_summary:") {
			return true
		}
	}
	return false
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
