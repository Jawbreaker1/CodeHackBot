package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func (r *Runner) ensureActionGoalTerminalState(goal string, dryRun bool) {
	goal = strings.TrimSpace(goal)
	if dryRun || goal == "" || !looksLikeAction(goal) {
		return
	}
	if strings.TrimSpace(r.pendingAssistGoal) != "" || strings.TrimSpace(r.pendingAssistQ) != "" {
		return
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return
	}
	if hasResultSnapshot(sessionDir) {
		return
	}
	refs := r.collectFinalizeEvidenceRefs()
	if len(refs) == 0 {
		return
	}

	if r.assistObjectiveMet {
		final := r.latestAssistantResultMessage(sessionDir)
		if strings.TrimSpace(final) == "" {
			final = "Objective appears satisfied from execution evidence."
		}
		trueVal := true
		suggestion := assist.Suggestion{
			Type:         "complete",
			Decision:     "step_complete",
			Final:        strings.TrimSpace(final),
			ObjectiveMet: &trueVal,
			EvidenceRefs: refs,
			WhyMet:       "objective inferred from execution evidence at finalize stage",
		}
		if err := r.executeAssistSuggestion(suggestion, false); err == nil && hasResultSnapshot(sessionDir) {
			return
		}
	}

	// Last-resort terminal state: explicit objective_not_met with concrete evidence refs.
	falseVal := false
	suggestion := assist.Suggestion{
		Type:         "complete",
		Decision:     "step_complete",
		Final:        "Objective not met from current evidence. Continue with a revised strategy if you want another attempt.",
		ObjectiveMet: &falseVal,
		EvidenceRefs: refs,
		WhyMet:       "session ended without a verifiable completion contract",
	}
	if err := r.executeAssistSuggestion(suggestion, false); err != nil && r.cfg.UI.Verbose {
		r.logger.Printf("Finalize terminal-state fallback failed: %v", err)
	}
}

func hasResultSnapshot(sessionDir string) bool {
	_, err := os.Stat(resultSnapshotPath(sessionDir))
	return err == nil
}

func (r *Runner) collectFinalizeEvidenceRefs() []string {
	refs := make([]string, 0, 6)
	seen := map[string]struct{}{}
	appendRef := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		refs = append(refs, path)
	}

	for _, artifact := range r.collectGoalEvalArtifacts(strings.TrimSpace(r.lastActionLogPath)) {
		appendRef(artifact.Path)
	}
	appendRef(strings.TrimSpace(r.lastActionLogPath))
	appendRef(strings.TrimSpace(r.lastSuccessLogPath))
	return refs
}

func (r *Runner) latestAssistantResultMessage(sessionDir string) string {
	chatPath := filepath.Join(sessionDir, "chat.log")
	data, err := os.ReadFile(chatPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "Assistant:") {
			continue
		}
		msg := strings.TrimSpace(strings.TrimPrefix(line, "Assistant:"))
		if msg == "" {
			continue
		}
		if strings.HasPrefix(msg, "Suggested command:") || strings.HasPrefix(msg, "Possible next steps:") {
			continue
		}
		return msg
	}
	return ""
}
