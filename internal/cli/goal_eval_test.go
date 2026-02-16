package cli

import (
	"path/filepath"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestParseGoalEvalResponseExtractsWrappedJSON(t *testing.T) {
	raw := "analysis...\n{\"done\":true,\"answer\":\"ok\",\"confidence\":\"high\"}\n"
	got, err := parseGoalEvalResponse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !got.Done {
		t.Fatalf("expected done=true")
	}
	if got.Answer != "ok" {
		t.Fatalf("unexpected answer: %q", got.Answer)
	}
}

func TestParseGoalEvalResponseNormalizesConfidence(t *testing.T) {
	raw := "{\"done\":false,\"answer\":\"\",\"confidence\":\"VERY\"}"
	got, err := parseGoalEvalResponse(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got.Confidence != "low" {
		t.Fatalf("expected low confidence fallback, got %q", got.Confidence)
	}
}

func TestShouldAttemptGoalEvaluationSkipsWriteCreationGoals(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-goal-eval", "", "")
	r.lastActionLogPath = filepath.Join(cfg.Session.LogDir, "demo.log")

	if r.shouldAttemptGoalEvaluation("create a syve.md report in owasp format") {
		t.Fatalf("expected goal evaluation to be skipped for write/create goals")
	}
}
