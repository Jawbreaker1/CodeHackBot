package cli

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestValidateAssistDecisionContract_ActionGoalRequiresDecision(t *testing.T) {
	r := NewRunner(config.Config{}, "session", "", "")
	r.assistRuntime.Goal = "scan localhost and report findings"

	err := r.validateAssistDecisionContract(assist.Suggestion{
		Type:    "command",
		Command: "nmap",
		Args:    []string{"-sV", "127.0.0.1"},
	})
	if err == nil {
		t.Fatalf("expected missing decision error")
	}
}

func TestValidateAssistDecisionContract_TypeMapping(t *testing.T) {
	r := NewRunner(config.Config{}, "session", "", "")
	r.assistRuntime.Goal = "scan localhost and report findings"

	cases := []struct {
		name       string
		suggestion assist.Suggestion
		wantErr    bool
	}{
		{
			name: "command retry modified",
			suggestion: assist.Suggestion{
				Type:     "command",
				Decision: "retry_modified",
				Command:  "nmap",
				Args:     []string{"-sV", "127.0.0.1"},
			},
		},
		{
			name: "command pivot strategy",
			suggestion: assist.Suggestion{
				Type:     "command",
				Decision: "pivot_strategy",
				Command:  "nmap",
				Args:     []string{"-sn", "127.0.0.1"},
			},
		},
		{
			name: "question ask user",
			suggestion: assist.Suggestion{
				Type:     "question",
				Decision: "ask_user",
				Question: "Which host should I scan?",
			},
		},
		{
			name: "complete step complete",
			suggestion: assist.Suggestion{
				Type:     "complete",
				Decision: "step_complete",
				Final:    "done",
			},
		},
		{
			name: "question invalid decision",
			suggestion: assist.Suggestion{
				Type:     "question",
				Decision: "pivot_strategy",
				Question: "Which host should I scan?",
			},
			wantErr: true,
		},
		{
			name: "complete invalid decision",
			suggestion: assist.Suggestion{
				Type:     "complete",
				Decision: "ask_user",
				Final:    "done",
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.validateAssistDecisionContract(tc.suggestion)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateAssistDecisionContract_ChatGoalSkipsDecision(t *testing.T) {
	r := NewRunner(config.Config{}, "session", "", "")
	r.assistRuntime.Goal = "hello there"
	if err := r.validateAssistDecisionContract(assist.Suggestion{Type: "complete", Final: "hi"}); err != nil {
		t.Fatalf("expected no error for conversational goal, got %v", err)
	}
}

func TestValidateAssistDecisionContract_OpenLikeRelaxesCommandDecision(t *testing.T) {
	cfg := config.Config{}
	cfg.Agent.AssistLoopMode = "open_like"
	r := NewRunner(cfg, "session", "", "")
	r.assistRuntime.Goal = "scan localhost and report findings"

	if err := r.validateAssistDecisionContract(assist.Suggestion{
		Type:    "command",
		Command: "nmap",
		Args:    []string{"-sV", "127.0.0.1"},
	}); err != nil {
		t.Fatalf("expected open_like to allow command without decision, got %v", err)
	}

	if err := r.validateAssistDecisionContract(assist.Suggestion{
		Type:     "complete",
		Decision: "ask_user",
		Final:    "done",
	}); err == nil {
		t.Fatalf("expected complete decision to still be enforced in open_like mode")
	}
}

func TestAssistRepairAttempts_DefaultsAndOverrides(t *testing.T) {
	r := NewRunner(config.Config{}, "session", "", "")
	if got := r.assistRepairAttempts(); got != 1 {
		t.Fatalf("expected strict default attempts=1, got %d", got)
	}

	cfgOpen := config.Config{}
	cfgOpen.Agent.AssistLoopMode = "open_like"
	rOpen := NewRunner(cfgOpen, "session-open", "", "")
	if got := rOpen.assistRepairAttempts(); got != 3 {
		t.Fatalf("expected open_like default attempts=3, got %d", got)
	}

	cfgOverride := config.Config{}
	cfgOverride.Agent.AssistLoopMode = "open_like"
	cfgOverride.Agent.AssistRepairAttempts = 5
	rOverride := NewRunner(cfgOverride, "session-override", "", "")
	if got := rOverride.assistRepairAttempts(); got != 5 {
		t.Fatalf("expected configured attempts=5, got %d", got)
	}
}

func TestNormalizeAssistDecision_CommandAndQuestion(t *testing.T) {
	r := NewRunner(config.Config{}, "session", "", "")
	r.assistRuntime.Goal = "scan localhost and report findings"

	cmd, note := r.normalizeAssistDecision(assist.Suggestion{
		Type:     "command",
		Decision: "step_complete",
		Command:  "nmap",
		Args:     []string{"-sV", "127.0.0.1"},
	})
	if cmd.Decision != "retry_modified" {
		t.Fatalf("expected command decision normalized to retry_modified, got %q", cmd.Decision)
	}
	if note == "" {
		t.Fatalf("expected normalization note for command")
	}

	q, note := r.normalizeAssistDecision(assist.Suggestion{
		Type:     "question",
		Decision: "pivot_strategy",
		Question: "target?",
	})
	if q.Decision != "ask_user" {
		t.Fatalf("expected question decision normalized to ask_user, got %q", q.Decision)
	}
	if note == "" {
		t.Fatalf("expected normalization note for question")
	}
}

func TestNormalizeAssistDecision_CompleteRemainsStrict(t *testing.T) {
	r := NewRunner(config.Config{}, "session", "", "")
	r.assistRuntime.Goal = "scan localhost and report findings"

	s, note := r.normalizeAssistDecision(assist.Suggestion{
		Type:     "complete",
		Decision: "ask_user",
		Final:    "done",
	})
	if s.Decision != "ask_user" {
		t.Fatalf("expected complete decision unchanged, got %q", s.Decision)
	}
	if note != "" {
		t.Fatalf("expected no normalization note for complete, got %q", note)
	}
	if err := r.validateAssistDecisionContract(s); err == nil {
		t.Fatalf("expected complete contract validation to still fail")
	}
}
