package workerloop

import "testing"

func TestParseResponseAction(t *testing.T) {
	r, err := ParseResponse(`{"type":"action","command":"ls -la","use_shell":true}`)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if r.Type != "action" || r.Command != "ls -la" || !r.UseShell {
		t.Fatalf("unexpected response: %#v", r)
	}
}

func TestParseResponseComplete(t *testing.T) {
	r, err := ParseResponse(`{"type":"step_complete","summary":"done"}`)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if r.Type != "step_complete" || r.Summary != "done" {
		t.Fatalf("unexpected response: %#v", r)
	}
}

func TestParseResponseRejectsInvalid(t *testing.T) {
	if _, err := ParseResponse(`{"type":"action"}`); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseResponseStripsThinkAndSupportsShellNeeded(t *testing.T) {
	r, err := ParseResponse("<think>reasoning</think>\n\n{\"type\":\"action\",\"command\":\"ls -la\",\"shell_needed\":true}")
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if !r.UseShell || r.Command != "ls -la" {
		t.Fatalf("unexpected response: %#v", r)
	}
}

func TestParseResponseUsesLastValidJSONContract(t *testing.T) {
	input := "<think>\nExample:\n{\"example\":true}\n</think>\n\n```json\n{\"type\":\"action\",\"command\":\"ls -la\",\"shell_needed\":true}\n```"

	r, err := ParseResponse(input)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if r.Type != "action" || r.Command != "ls -la" || !r.UseShell {
		t.Fatalf("unexpected response: %#v", r)
	}
}

func TestParseResponseSupportsNestedActionEnvelope(t *testing.T) {
	r, err := ParseResponse(`{"action":{"command":"ls -la","shell_needed":true}}`)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if r.Type != "action" || r.Command != "ls -la" || !r.UseShell {
		t.Fatalf("unexpected response: %#v", r)
	}
}

func TestParseResponseSupportsBooleanStepCompleteEnvelope(t *testing.T) {
	r, err := ParseResponse(`{"action":null,"step_complete":true,"reason":"done"}`)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if r.Type != "step_complete" || r.Summary != "done" {
		t.Fatalf("unexpected response: %#v", r)
	}
}

func TestParseResponseSupportsStringStepCompleteEnvelope(t *testing.T) {
	r, err := ParseResponse(`{"action":"step_complete","reasoning":"done"}`)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if r.Type != "step_complete" || r.Summary != "done" {
		t.Fatalf("unexpected response: %#v", r)
	}
}
