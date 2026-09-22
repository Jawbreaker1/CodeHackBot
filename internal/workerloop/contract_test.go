package workerloop

import "testing"

func TestDecisionContract(t *testing.T) {
	for _, text := range []string{
		`{"type":"action","command":"printf","args":["%s","literal; text"]}`,
		`{"type":"action","command":"node","args":["runner.mjs"],"artifacts":["screenshots/home.png","traces/run.zip"]}`,
		`{"type":"action","command":"printf '%s' text","use_shell":true}`,
		`{"type":"step_complete","summary":"supported answer"}`,
		`{"type":"ask_user","question":"Which file?"}`,
		`{"type":"blocked","summary":"Need missing input"}`,
		`{"type":"update_plan","plan":{"summary":"changed observation","steps":["inspect","verify"],"active_step":"verify"}}`,
	} {
		if _, err := ParseResponse(text); err != nil {
			t.Errorf("%s: %v", text, err)
		}
	}
	for _, text := range []string{
		`{"action":{"command":"pwd"}}`,
		`{"type":"step_complete","summary":"done","command":"touch x"}`,
		`{"type":"action","command":"echo x","use_shell":true,"args":["y"]}`,
		`{"type":"update_plan"}`,
		`{"type":"update_plan","plan":{"summary":"x","steps":["a","a"],"active_step":"a"}}`,
		`{"type":"update_plan","plan":{"summary":"x","steps":["a"],"active_step":"b"}}`,
		`{"type":"action","command":"pwd","scope":"new scope"}`,
		`{"type":"step_complete","summary":"done","artifacts":["report.md"]}`,
		`{"type":"action","command":"pwd","artifacts":[""]}`,
		`{"type":"action","command":"pwd"} {"type":"step_complete","summary":"done"}`,
		`{"type":"step_complete"}`,
	} {
		if _, err := ParseResponse(text); err == nil {
			t.Errorf("accepted ambiguous or invalid decision: %s", text)
		}
	}
}
