package orchestrator

import "testing"

func TestBuildAssistPromptScopeFallsBackToTaskTargetsWhenRunScopeEmpty(t *testing.T) {
	t.Parallel()

	scope := buildAssistPromptScope(Scope{}, []string{"192.168.50.10"})
	if len(scope) != 1 || scope[0] != "192.168.50.10" {
		t.Fatalf("expected fallback task target scope, got %#v", scope)
	}
}

func TestBuildAssistPromptScopeIncludesDenyMarkers(t *testing.T) {
	t.Parallel()

	scope := buildAssistPromptScope(Scope{
		Networks:    []string{"192.168.50.0/24"},
		Targets:     []string{"corp.internal"},
		DenyTargets: []string{"192.168.50.99"},
	}, []string{"192.168.50.10"})
	if len(scope) != 3 {
		t.Fatalf("expected full run scope entries, got %#v", scope)
	}
	if scope[0] != "192.168.50.0/24" || scope[1] != "corp.internal" || scope[2] != "deny:192.168.50.99" {
		t.Fatalf("unexpected run scope prompt entries: %#v", scope)
	}
}
