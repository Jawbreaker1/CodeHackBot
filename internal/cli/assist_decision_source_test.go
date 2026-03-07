package cli

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func TestClassifyAssistDecisionSource(t *testing.T) {
	if got := classifyAssistDecisionSource(false, assist.LLMSuggestMetadata{}, false); got != decisionSourceStatic {
		t.Fatalf("expected static fallback when no llm meta, got %q", got)
	}
	if got := classifyAssistDecisionSource(true, assist.LLMSuggestMetadata{ParseRepairUsed: true}, false); got != decisionSourceLLMRepair {
		t.Fatalf("expected llm_repair, got %q", got)
	}
	if got := classifyAssistDecisionSource(true, assist.LLMSuggestMetadata{}, false); got != decisionSourceLLMDirect {
		t.Fatalf("expected llm_direct, got %q", got)
	}
	if got := classifyAssistDecisionSource(true, assist.LLMSuggestMetadata{}, true); got != decisionSourceStatic {
		t.Fatalf("expected static fallback when fallback used, got %q", got)
	}
}

func TestReadAssistDecisionSourceMix(t *testing.T) {
	r := &Runner{sessionID: "s-1"}
	sessionDir := t.TempDir()
	if err := r.appendAssistDecisionSource(sessionDir, decisionSourceLLMDirect, "execute-step", "command", "nmap"); err != nil {
		t.Fatalf("append llm_direct: %v", err)
	}
	if err := r.appendAssistDecisionSource(sessionDir, decisionSourceLLMRepair, "recover", "command", "nmap"); err != nil {
		t.Fatalf("append llm_repair: %v", err)
	}
	if err := r.appendAssistDecisionSource(sessionDir, decisionSourceRuntimeAdapt, "execute-step", "adapt", "nmap"); err != nil {
		t.Fatalf("append runtime_adapt: %v", err)
	}
	counts, total, err := readAssistDecisionSourceMix(sessionDir)
	if err != nil {
		t.Fatalf("read mix: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if counts[decisionSourceLLMDirect] != 1 || counts[decisionSourceLLMRepair] != 1 || counts[decisionSourceRuntimeAdapt] != 1 {
		t.Fatalf("unexpected counts: %#v", counts)
	}
}
