package cli

import (
	"errors"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

func TestShouldCountLLMFailure(t *testing.T) {
	if shouldCountLLMFailure(nil) {
		t.Fatalf("nil error should not count")
	}
	if !shouldCountLLMFailure(errors.New("connection refused")) {
		t.Fatalf("transport error should count")
	}
	if shouldCountLLMFailure(assist.SuggestionParseError{Err: errors.New("bad json"), Raw: "oops"}) {
		t.Fatalf("parse error should not count")
	}
}
