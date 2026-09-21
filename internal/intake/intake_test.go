package intake

import "testing"

func TestDecodeTurnRequiresExactProtocol(t *testing.T) {
	turn, err := DecodeTurn(`{"reply":"I coordinate authorized assessments.","proposal":null}`)
	if err != nil || turn.Reply == "" || turn.Proposal != nil {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	if _, err := DecodeTurn("I coordinate authorized assessments."); err == nil {
		t.Fatal("plain text must not be treated as a control response")
	}
	if _, err := DecodeTurn(`{"reply":"ready","proposal":{"goal":"","scope":"target"}}`); err == nil {
		t.Fatal("incomplete proposal must be rejected")
	}
	turn, err = DecodeTurn(`{"reply":"I will inspect the workspace entries first.","proposal":null,"tool":{"name":"list_directory","path":"."}}`)
	if err != nil || turn.Tool == nil || turn.Tool.Name != "list_directory" {
		t.Fatalf("tool turn=%+v err=%v", turn, err)
	}
	turn, err = DecodeTurn(`{"reply":"I will inspect fixed host metadata.","proposal":null,"tool":{"name":"host_system"}}`)
	if err != nil || turn.Tool == nil || turn.Tool.Name != "host_system" {
		t.Fatalf("host tool turn=%+v err=%v", turn, err)
	}
	if _, err := DecodeTurn(`{"reply":"choose one","proposal":{"goal":"check","scope":"lab"},"tool":{"name":"local_network"}}`); err == nil {
		t.Fatal("tool and proposal must not be combined")
	}
}
