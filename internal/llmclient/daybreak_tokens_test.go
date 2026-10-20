package llmclient

import (
	"strings"
	"sync"
	"testing"
)

func TestDaybreakTextCounterAgainstMeasuredBridgeShapes(t *testing.T) {
	const system = "This is a harmless context-size measurement. Reply with OK only. Do not run tools."
	cases := []struct {
		name     string
		messages []Message
		measured int
	}{
		{"prose", []Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("The fictional service recorded a routine observation. The analyst retained its source and uncertainty.\n", 400)}}, 6428},
		{"code", []Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("func inspectRecord(input []byte) (int, error) { if len(input) == 0 { return 0, nil }; return len(input), nil }\n", 350)}}, 11578},
		{"log", []Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("2026-01-01T00:00:00Z fixture INFO request_id=7d3a9b2e status=200 path=/sample/detail elapsed_ms=17\n", 400)}}, 15228},
		{"unicode", []Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("Fiktiv granskning: åtgärd, säkerhet, spårbarhet. 日本語の記録。🔎\n", 500)}}, 14528},
		{"json", []Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat(`{"fixture_id":"observation-001","status":"recorded","ports":[80,443],"verified":false}`+"\n", 450)}}, 10828},
		{"multi-turn", []Message{{Role: "system", Content: system}, {Role: "user", Content: strings.Repeat("First fictional observation.\n", 350)}, {Role: "assistant", Content: "The first observation was recorded."}, {Role: "user", Content: strings.Repeat("Second fictional observation.\n", 350)}}, 2844},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			got := DaybreakTextInputTokens(item.messages)
			if got < item.measured || got > item.measured*103/100+40 {
				t.Fatalf("preflight=%d, measured provider=%d", got, item.measured)
			}
		})
	}
}

func TestDaybreakAssessmentBudgetDoesNotChangeOtherClients(t *testing.T) {
	client := Client{Model: "gpt-daybreak-blue-latest", AuthTokenFile: "/tmp/bridge-token", MaxInputBytes: SubscriptionInputByteLimit}
	assessment := client.WithDaybreakAssessmentBudget()
	if assessment.MaxInputTokens != DaybreakAssessmentInputTokens || assessment.InputByteLimit() != DaybreakAssessmentInputBytes || assessment.CountInputTokens == nil {
		t.Fatalf("assessment limits = %+v", assessment)
	}
	if client.MaxInputTokens != 0 || client.InputByteLimit() != SubscriptionInputByteLimit {
		t.Fatal("the caller's intake/vision client changed")
	}
	if got := (Client{Model: client.Model, AuthTokenFile: client.AuthTokenFile, MaxInputBytes: 48 * 1024}).WithDaybreakAssessmentBudget(); got.InputByteLimit() != 48*1024 {
		t.Fatal("an explicitly smaller byte cap was silently raised")
	}
	if got := DaybreakTextInputTokens([]Message{{Role: "user", Content: "image", Attachments: []Attachment{{MIMEType: "image/png", Data: []byte{1}}}}}); got != 0 {
		t.Fatalf("attachment was counted as plain text: %d", got)
	}
	for _, other := range []Client{{Model: "qwen/qwen3.8-27b"}, {Model: "gpt-daybreak-blue-latest"}, {Model: "other", AuthTokenFile: "bridge"}} {
		if got := other.WithDaybreakAssessmentBudget(); got.MaxInputTokens != 0 || got.CountInputTokens != nil {
			t.Fatalf("other model changed: %+v", got)
		}
	}
}

func TestDaybreakTextCounterConcurrent(t *testing.T) {
	messages := []Message{{Role: "system", Content: "Guide the assessment."}, {Role: "user", Content: strings.Repeat("A saved observation remains available. ", 100)}}
	want := DaybreakTextInputTokens(messages)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if got := DaybreakTextInputTokens(messages); got != want {
				t.Errorf("concurrent count = %d, want %d", got, want)
			}
		}()
	}
	group.Wait()
}
