package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type completionEvidenceCheck struct {
	Verified bool     `json:"verified"`
	Reason   string   `json:"reason"`
	Missing  []string `json:"missing"`
}

func (r *Runner) resolveCompletionEvidenceRefs(refs []string) ([]string, []string) {
	if len(refs) == 0 {
		return nil, nil
	}
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	resolved := make([]string, 0, len(refs))
	missing := make([]string, 0)
	seen := map[string]struct{}{}

	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		candidates := completionRefCandidates(ref, sessionDir)
		found := ""
		for _, candidate := range candidates {
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			found = candidate
			break
		}
		if found == "" {
			missing = append(missing, ref)
			continue
		}
		if _, ok := seen[found]; ok {
			continue
		}
		seen[found] = struct{}{}
		resolved = append(resolved, found)
	}
	return resolved, missing
}

func (r *Runner) shouldEnforceResolvedEvidenceRefs(refs []string) bool {
	sessionID := strings.TrimSpace(r.sessionID)
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if filepath.IsAbs(ref) {
			return true
		}
		if sessionID != "" && strings.Contains(ref, sessionID) {
			return true
		}
		if _, err := os.Stat(ref); err == nil {
			return true
		}
	}
	return false
}

func completionRefCandidates(ref string, sessionDir string) []string {
	ref = filepath.Clean(strings.TrimSpace(ref))
	if ref == "" {
		return nil
	}
	if filepath.IsAbs(ref) {
		return []string{ref}
	}
	candidates := []string{ref}
	if sessionDir != "" {
		candidates = append(candidates, filepath.Join(sessionDir, ref))
	}
	return candidates
}

func (r *Runner) verifyCompletionEvidenceSemantics(goal string, suggestion assist.Suggestion, refs []string) error {
	if len(refs) == 0 {
		return fmt.Errorf("assistant completion contract: no resolved evidence refs")
	}
	if !r.llmAllowed() {
		// Transport offline/cooldown: rely on structural contract + concrete local evidence refs.
		return nil
	}
	input := buildCompletionEvidencePrompt(goal, suggestion, refs)
	if strings.TrimSpace(input) == "" {
		return fmt.Errorf("assistant completion contract: no evidence content available for semantic verification")
	}

	client := llm.NewLMStudioClient(r.cfg)
	temp, maxTokens := r.llmRoleOptions("recovery", 0.05, 900)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("completion verify")
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: temp,
		MaxTokens:   maxTokens,
		Messages: []llm.Message{
			{
				Role: "system",
				Content: "Return JSON only: " +
					`{"verified":true|false,"reason":"string","missing":["string"]}. ` +
					"Set verified=true only if provided evidence directly proves the goal is complete.",
			},
			{
				Role:    "user",
				Content: input,
			},
		},
	})
	stopIndicator()
	if err != nil {
		// Don't regress runtime stability on transient LLM outages.
		if r.cfg.UI.Verbose {
			r.logger.Printf("Completion semantic verification skipped: %v", err)
		}
		return nil
	}
	parsed, parseErr := parseCompletionEvidenceCheck(resp.Content)
	if parseErr != nil {
		return fmt.Errorf("assistant completion contract: semantic verification parse failure: %w", parseErr)
	}
	if parsed.Verified {
		return nil
	}
	reason := strings.TrimSpace(parsed.Reason)
	if reason == "" {
		reason = "evidence does not yet prove the objective"
	}
	if len(parsed.Missing) > 0 {
		return fmt.Errorf("assistant completion contract: objective proof incomplete (%s). missing: %s", reason, strings.Join(parsed.Missing, "; "))
	}
	return fmt.Errorf("assistant completion contract: objective proof incomplete (%s)", reason)
}

func buildCompletionEvidencePrompt(goal string, suggestion assist.Suggestion, refs []string) string {
	builder := strings.Builder{}
	builder.WriteString("Goal:\n")
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n\nCompletion claim:\n")
	builder.WriteString("why_met: " + strings.TrimSpace(suggestion.WhyMet) + "\n")
	if final := strings.TrimSpace(suggestion.Final); final != "" {
		builder.WriteString("final: " + final + "\n")
	}
	if checklist := objectiveChecklist(goal); len(checklist) > 0 {
		builder.WriteString("Objective checklist:\n")
		for _, item := range checklist {
			builder.WriteString("- " + item + "\n")
		}
	}
	builder.WriteString("\nEvidence snippets:\n")
	added := 0
	for _, path := range refs {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		const maxBytes = 2200
		if len(data) > maxBytes {
			data = data[len(data)-maxBytes:]
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		added++
		builder.WriteString("\n[path: " + path + "]\n")
		builder.WriteString(content + "\n")
	}
	if added == 0 {
		return ""
	}
	return builder.String()
}

func parseCompletionEvidenceCheck(raw string) (completionEvidenceCheck, error) {
	blob := strings.TrimSpace(raw)
	if obj, ok := extractJSONObject(blob); ok {
		blob = obj
	}
	var out completionEvidenceCheck
	if err := json.Unmarshal([]byte(blob), &out); err != nil {
		return completionEvidenceCheck{}, fmt.Errorf("parse completion verify json: %w", err)
	}
	out.Reason = strings.TrimSpace(out.Reason)
	cleanMissing := make([]string, 0, len(out.Missing))
	for _, item := range out.Missing {
		item = strings.TrimSpace(item)
		if item != "" {
			cleanMissing = append(cleanMissing, item)
		}
	}
	out.Missing = cleanMissing
	return out, nil
}
