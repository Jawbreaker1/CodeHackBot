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
}
