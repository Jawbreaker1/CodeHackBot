package workermode

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
)

func TestParseDecision(t *testing.T) {
	raw := `{"mode":"direct_execution","reason":"simple one-step file listing request"}`
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Mode != workerplan.ModeDirectExecution {
		t.Fatalf("mode = %q, want %q", got.Mode, workerplan.ModeDirectExecution)
	}
}

func TestValidateDecisionAcceptsSupportedModes(t *testing.T) {
	for _, decision := range []Decision{
		{Mode: workerplan.ModeConversation, Reason: "user asked for explanation only"},
		{Mode: workerplan.ModeDirectExecution, Reason: "simple one-step listing request"},
		{Mode: workerplan.ModePlannedExecution, Reason: "task requires multiple semantic phases"},
	} {
		if report := Validate(decision); !report.Valid() {
			t.Fatalf("Validate(%+v) unexpected error = %v", decision, report.Error())
		}
	}
}

func TestValidateDecisionRejectsInvalidShape(t *testing.T) {
	report := Validate(Decision{Mode: "maybe", Reason: ""})
	if report.Valid() {
		t.Fatalf("Validate() expected invalid report")
	}
	for _, want := range []string{"unsupported worker mode", "worker mode requires reason"} {
		if !strings.Contains(report.Error().Error(), want) {
			t.Fatalf("report missing %q in %v", want, report.Error())
		}
	}
}

func TestValidateDecisionRejectsStructuredReasonBlob(t *testing.T) {
	report := Validate(Decision{Mode: workerplan.ModeConversation, Reason: `{"mode":"conversation"}`})
	if report.Valid() {
		t.Fatalf("Validate() expected invalid report")
	}
	if !strings.Contains(report.Error().Error(), "worker mode reason must be a short explanation") {
		t.Fatalf("unexpected error = %v", report.Error())
	}
}

func TestFallbackDecisionIsSafeConversation(t *testing.T) {
	got := FallbackDecision()
	if got.Mode != workerplan.ModeConversation {
		t.Fatalf("mode = %q, want %q", got.Mode, workerplan.ModeConversation)
	}
	if strings.TrimSpace(got.Reason) == "" {
		t.Fatalf("reason must not be empty")
	}
}

func TestNormalizeConciseReasonReplacesVerboseReasonForSupportedMode(t *testing.T) {
	got := NormalizeConciseReason(Decision{
		Mode:   workerplan.ModePlannedExecution,
		Reason: strings.Repeat("too many words ", 20),
	})
	if got.Mode != workerplan.ModePlannedExecution {
		t.Fatalf("mode = %q", got.Mode)
	}
	if got.Reason != "task requires multiple execution phases" {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestNormalizeConciseReasonLeavesStructuredNoiseUntouched(t *testing.T) {
	raw := `{"mode":"conversation"}`
	got := NormalizeConciseReason(Decision{
		Mode:   workerplan.ModeConversation,
		Reason: raw,
	})
	if got.Reason != raw {
		t.Fatalf("reason = %q, want original", got.Reason)
	}
}
