package orchestrator

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func TestValidateAssistSuggestionSchemaRejectsInvalidToolShape(t *testing.T) {
	t.Parallel()

	err := validateAssistSuggestionSchema(assist.Suggestion{
		Type: "tool",
		Tool: &assist.ToolSpec{
			Run: assist.ToolRun{Command: "python3"},
		},
	})
	if err == nil {
		t.Fatalf("expected schema validation error for empty tool files")
	}
}

func TestValidateAssistSuggestionSchemaRejectsEmptySteps(t *testing.T) {
	t.Parallel()

	err := validateAssistSuggestionSchema(assist.Suggestion{
		Type:  "plan",
		Steps: []string{"enumerate target", "  "},
	})
	if err == nil {
		t.Fatalf("expected schema validation error for empty step")
	}
}

func TestValidateAssistSuggestionSchemaAcceptsValidToolSuggestion(t *testing.T) {
	t.Parallel()

	err := validateAssistSuggestionSchema(assist.Suggestion{
		Type: "tool",
		Tool: &assist.ToolSpec{
			Files: []assist.ToolFile{
				{Path: "collect/main.py", Content: "print('ok')"},
			},
			Run: assist.ToolRun{
				Command: "python3",
				Args:    []string{"collect/main.py"},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected valid tool schema, got %v", err)
	}
}
