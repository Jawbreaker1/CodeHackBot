package guided

import "testing"

func TestDecodeIntakeTurnRequiresExactProtocol(t *testing.T) {
	turn, err := decodeIntakeTurn(`{"reply":"I coordinate authorized assessments.","proposal":null}`)
	if err != nil || turn.Reply == "" || turn.Proposal != nil {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	if _, err := decodeIntakeTurn("I coordinate authorized assessments."); err == nil {
		t.Fatal("plain text must not be treated as a control response")
	}
	if _, err := decodeIntakeTurn(`{"reply":"ready","proposal":{"goal":"","scope":"target"}}`); err == nil {
		t.Fatal("incomplete proposal must be rejected")
	}
}
