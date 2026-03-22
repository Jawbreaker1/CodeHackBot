package tokenutil

import "testing"

func TestApproxTokens(t *testing.T) {
	if got := ApproxTokens(""); got != 0 {
		t.Fatalf("ApproxTokens(empty) = %d", got)
	}
	if got := ApproxTokens("abcd"); got != 1 {
		t.Fatalf("ApproxTokens(4 chars) = %d", got)
	}
	if got := ApproxTokens("abcde"); got != 2 {
		t.Fatalf("ApproxTokens(5 chars) = %d", got)
	}
}
