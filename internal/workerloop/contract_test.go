package workerloop

import "testing"

func TestDecisionContract(t *testing.T) {
	for _, text := range []string{
		`{"type":"bash","command":"printf","args":["%s","literal; text"]}`,
		`{"type":"bash","command":"node","args":["runner.mjs"],"artifacts":["screenshots/home.png","traces/run.zip"]}`,
		`{"type":"bash","command":"printf '%s' text","use_shell":true}`,
		`{"type":"bash","command":"rm","args":["--","/tmp/one.txt"],"summary":"Remove one reviewed file","target":"/tmp/one.txt","risk":"dangerous","impact":"Deletes exactly that file"}`,
		`{"type":"browser_run","scenario":"check.mjs","artifact_dir":"browser-check-1","summary":"Inspect the sign-in page","target":"local fixture","impact":"One browser visit; no form submitted","risk":"low"}`,
		`{"type":"step_complete","summary":"supported answer"}`,
		`{"type":"step_complete","summary":"supported answer","lead_hints":["The observed export path may expose another tenant's data; compare a synthetic marker."]}`,
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
		`{"type":"action","command":"pwd"}`,
		`{"type":"step_complete","summary":"done","command":"touch x"}`,
		`{"type":"bash","command":"echo x","use_shell":true,"args":["y"]}`,
		`{"type":"update_plan"}`,
		`{"type":"update_plan","plan":{"summary":"x","steps":["a","a"],"active_step":"a"}}`,
		`{"type":"update_plan","plan":{"summary":"x","steps":["a"],"active_step":"b"}}`,
		`{"type":"bash","command":"pwd","scope":"new scope"}`,
		`{"type":"step_complete","summary":"done","artifacts":["report.md"]}`,
		`{"type":"bash","command":"pwd","lead_hints":["unreviewed lead"]}`,
		`{"type":"step_complete","summary":"done","lead_hints":["", "second"]}`,
		`{"type":"bash","command":"pwd","artifacts":[""]}`,
		`{"type":"browser_run","scenario":"check.mjs","artifact_dir":"browser-check-1","command":"node","summary":"Inspect","target":"fixture","impact":"visit"}`,
		`{"type":"browser_run","scenario":"check.mjs","artifact_dir":"browser-check-1","summary":"Inspect","target":"fixture"}`,
		`{"type":"bash","command":"pwd","scenario":"check.mjs"}`,
		`{"type":"bash","command":"pwd"} {"type":"step_complete","summary":"done"}`,
		`{"type":"delete_file","path":"/tmp/one.txt","summary":"Unsupported special tool","impact":"Deletes file"}`,
		`{"type":"step_complete"}`,
	} {
		if _, err := ParseResponse(text); err == nil {
			t.Errorf("accepted ambiguous or invalid decision: %s", text)
		}
	}
}
