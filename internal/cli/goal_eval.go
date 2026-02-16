package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
)

type goalEvalResult struct {
	Done       bool   `json:"done"`
	Answer     string `json:"answer"`
	Confidence string `json:"confidence"`
	Reason     string `json:"reason"`
	Next       string `json:"next"`
}

func (r *Runner) shouldAttemptGoalEvaluation(goal string) bool {
	goal = strings.TrimSpace(strings.ToLower(goal))
	if goal == "" {
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
	fmt.Println(answer)
	r.appendConversation("Assistant", answer)
	r.pendingAssistGoal = ""
	r.pendingAssistQ = ""
	return true
}

func (r *Runner) evaluateGoalFromLatestArtifact(goal string) (goalEvalResult, error) {
	path := strings.TrimSpace(r.lastActionLogPath)
	if path == "" {
		return goalEvalResult{}, fmt.Errorf("latest artifact path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return goalEvalResult{}, fmt.Errorf("read artifact: %w", err)
	}
	const maxBytes = 16_000
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}

	prompt := strings.Builder{}
	prompt.WriteString("User goal:\n")
	prompt.WriteString(strings.TrimSpace(goal))
	prompt.WriteString("\n\nLatest artifact path:\n")
	prompt.WriteString(path)
	prompt.WriteString("\n\nLatest artifact content:\n")
	prompt.WriteString(string(data))
	prompt.WriteString("\n\nDecide if the goal is already satisfied by this artifact alone.")

	client := llm.NewLMStudioClient(r.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("eval")
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: 0,
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
