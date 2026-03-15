package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type goalEvalResult struct {
	Done       bool   `json:"done"`
	Answer     string `json:"answer"`
	Confidence string `json:"confidence"`
	Reason     string `json:"reason"`
	Next       string `json:"next"`
}

type goalEvalArtifact struct {
	Path    string
	Content string
}

func (r *Runner) shouldAttemptGoalEvaluation(goal string) bool {
	goal = strings.TrimSpace(strings.ToLower(goal))
	if goal == "" {
		return false
	}
	if isWriteCreationIntent(goal) {
		return false
	}
	if strings.TrimSpace(r.lastActionLogPath) == "" {
		return false
	}
	// Prioritize evaluation for answer-seeking goals.
	hints := []string{
		"what", "show", "tell", "summar", "check", "verify", "is there", "does",
		"list", "find", "give me", "report", "contents", "content",
	}
	for _, hint := range hints {
		if strings.Contains(goal, hint) {
			return true
		}
	}
	return false
}

func (r *Runner) tryConcludeGoalFromArtifacts(goal string) bool {
	if !r.llmAllowed() || !r.shouldAttemptGoalEvaluation(goal) {
		return false
	}
	result, err := r.evaluateGoalFromLatestArtifact(goal)
	if err != nil {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Goal evaluation skipped: %v", err)
		}
		return false
	}
	if !result.Done {
		return false
	}
	answer := strings.TrimSpace(result.Answer)
	if answer == "" {
		answer = "Goal appears satisfied based on the latest step output."
	}
	answer = normalizeAssistantOutput(answer)
	if r.tryEnforceCompletionContractFromEval(goal, answer, result) {
		r.assistObjectiveMet = true
		r.pendingAssistGoal = ""
		r.pendingAssistQ = ""
		return true
	}
	safePrintln(answer)
	r.appendConversation("Assistant", answer)
	r.assistObjectiveMet = true
	if bridgeErr := r.recordBridgedCompletion(goal, answer, result); bridgeErr != nil && r.cfg.UI.Verbose {
		r.logger.Printf("Bridged completion snapshot failed: %v", bridgeErr)
	}
	r.pendingAssistGoal = ""
	r.pendingAssistQ = ""
	return true
}

func (r *Runner) tryEnforceCompletionContractFromEval(goal, answer string, result goalEvalResult) bool {
	if strings.TrimSpace(goal) == "" || !r.llmAllowed() {
		return false
	}
	artifacts := r.collectGoalEvalArtifacts(r.lastActionLogPath)
	refs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if path := strings.TrimSpace(artifact.Path); path != "" {
			refs = append(refs, path)
		}
	}
	if len(refs) == 0 {
		return false
	}
	why := strings.TrimSpace(result.Reason)
	if why == "" {
		why = "objective verified from evidence-based goal evaluation"
	}
	goalPrompt := strings.Builder{}
	goalPrompt.WriteString("Completion contract enforcement:\n")
	goalPrompt.WriteString("The objective is already satisfied by evidence. Do not execute any command.\n")
	goalPrompt.WriteString("Return exactly one JSON suggestion with type=complete, decision=step_complete, objective_met=true, why_met, and evidence_refs.\n")
	goalPrompt.WriteString("User goal: " + strings.TrimSpace(goal) + "\n")
	goalPrompt.WriteString("Expected final answer: " + strings.TrimSpace(answer) + "\n")
	goalPrompt.WriteString("Why objective is met: " + why + "\n")
	goalPrompt.WriteString("Allowed evidence refs:\n")
	for _, ref := range refs {
		goalPrompt.WriteString("- " + ref + "\n")
	}
	stopIndicator := r.startLLMIndicatorIfAllowed("complete-contract")
	suggestion, err := r.getAssistSuggestion(goalPrompt.String(), "recover")
	stopIndicator()
	if err != nil {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Completion contract enforcement skipped: %v", err)
		}
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(suggestion.Type), "complete") {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Completion contract enforcement received type=%s (expected complete)", strings.TrimSpace(suggestion.Type))
		}
		return false
	}
	if err := r.executeAssistSuggestion(suggestion, false); err != nil {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Completion contract enforcement rejected: %v", err)
		}
		return false
	}
	return true
}

func (r *Runner) evaluateGoalFromLatestArtifact(goal string) (goalEvalResult, error) {
	path := strings.TrimSpace(r.lastActionLogPath)
	if path == "" {
		return goalEvalResult{}, fmt.Errorf("latest artifact path is empty")
	}
	artifacts := r.collectGoalEvalArtifacts(path)
	if len(artifacts) == 0 {
		return goalEvalResult{}, fmt.Errorf("no readable artifacts for goal evaluation")
	}

	prompt := strings.Builder{}
	prompt.WriteString("User goal:\n")
	prompt.WriteString(strings.TrimSpace(goal))
	prompt.WriteString("\n\nArtifact evidence (most recent first):\n")
	for _, artifact := range artifacts {
		prompt.WriteString("\n---\nPath: ")
		prompt.WriteString(artifact.Path)
		prompt.WriteString("\nContent:\n")
		prompt.WriteString(artifact.Content)
		prompt.WriteString("\n")
	}
	prompt.WriteString("\nDecide if the goal is already satisfied by this evidence.")

	client := llm.NewLMStudioClient(r.cfg)
	temperature, maxTokens := r.llmRoleOptions("recovery", 0.05, 900)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("eval")
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Messages: []llm.Message{
			{
				Role: "system",
				Content: "Return JSON only with schema: " +
					`{"done":true|false,"answer":"string","confidence":"low|medium|high","reason":"string","next":"string"}. ` +
					"Set done=true only if the artifact directly satisfies the goal.",
			},
			{
				Role:    "user",
				Content: prompt.String(),
			},
		},
	})
	stopIndicator()
	if err != nil {
		r.recordLLMFailure(err)
		return goalEvalResult{}, err
	}

	parsed, parseErr := parseGoalEvalResponse(resp.Content)
	if parseErr != nil {
		if r.cfg.UI.Verbose {
			r.logger.Printf("Goal evaluation parse failed: %v", parseErr)
		}
		return goalEvalResult{}, parseErr
	}
	r.recordLLMSuccess()
	return parsed, nil
}

func (r *Runner) collectGoalEvalArtifacts(primaryPath string) []goalEvalArtifact {
	const (
		maxArtifacts      = 4
		maxBytesPerFile   = 6_000
		sessionLogSubdir  = "logs"
	)
	artifacts := make([]goalEvalArtifact, 0, maxArtifacts)
	seen := map[string]bool{}
	addPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] || len(artifacts) >= maxArtifacts {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if len(data) > maxBytesPerFile {
			data = data[len(data)-maxBytesPerFile:]
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			return
		}
		artifacts = append(artifacts, goalEvalArtifact{
			Path:    path,
			Content: content,
		})
		seen[path] = true
	}

	addPath(primaryPath)

	logDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID, sessionLogSubdir)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return artifacts
	}
	type candidate struct {
		path string
		mod  time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{
			path: filepath.Join(logDir, entry.Name()),
			mod:  info.ModTime(),
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].mod.Equal(candidates[j].mod) {
			return candidates[i].path > candidates[j].path
		}
		return candidates[i].mod.After(candidates[j].mod)
	})
	for _, candidate := range candidates {
		if len(artifacts) >= maxArtifacts {
			break
		}
		addPath(candidate.path)
	}
	return artifacts
}

func (r *Runner) recordBridgedCompletion(goal, finalAnswer string, result goalEvalResult) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	artifacts := r.collectGoalEvalArtifacts(r.lastActionLogPath)
	evidenceRefs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if path := strings.TrimSpace(artifact.Path); path != "" {
			evidenceRefs = append(evidenceRefs, path)
		}
	}
	why := strings.TrimSpace(result.Reason)
	if why == "" {
		why = "objective verified from evidence-based goal evaluation"
	}
	trueVal := true
	suggestion := assist.Suggestion{
		Type:         "complete",
		Decision:     "step_complete",
		Final:        strings.TrimSpace(finalAnswer),
		ObjectiveMet: &trueVal,
		EvidenceRefs: evidenceRefs,
		WhyMet:       why,
	}
	if err := r.appendTaskJournalEntry(sessionDir, taskJournalEntry{
		Kind:         "complete",
		Result:       "ok",
		Decision:     "step_complete",
		EvidenceRefs: evidenceRefs,
		Excerpt:      strings.TrimSpace(finalAnswer),
		Notes:        why,
	}); err != nil {
		return err
	}
	if err := r.writeResultSnapshot(strings.TrimSpace(goal), suggestion); err != nil {
		return err
	}
	r.refreshFocusFromRecentState(sessionDir, strings.TrimSpace(goal))
	return nil
}

func parseGoalEvalResponse(raw string) (goalEvalResult, error) {
	jsonBlob := strings.TrimSpace(raw)
	if obj, ok := extractJSONObject(jsonBlob); ok {
		jsonBlob = obj
	}
	var result goalEvalResult
	if err := json.Unmarshal([]byte(jsonBlob), &result); err != nil {
		return goalEvalResult{}, fmt.Errorf("parse goal eval json: %w", err)
	}
	result.Answer = strings.TrimSpace(result.Answer)
	result.Reason = strings.TrimSpace(result.Reason)
	result.Next = strings.TrimSpace(result.Next)
	result.Confidence = strings.ToLower(strings.TrimSpace(result.Confidence))
	switch result.Confidence {
	case "", "low", "medium", "high":
	default:
		result.Confidence = "low"
	}
	return result, nil
}

func extractJSONObject(text string) (string, bool) {
	start := -1
	depth := 0
	inString := false
	escape := false
	for i, r := range text {
		if start == -1 {
			if r == '{' {
				start = i
				depth = 1
				inString = false
				escape = false
			}
			continue
		}
		if inString {
			if escape {
				escape = false
				continue
			}
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : i+1]), true
			}
		}
	}
	return "", false
}
