package context

import (
	"strings"
	"testing"
)

func TestAppendConversationRetainsAllTurnsInAuthoritativePacket(t *testing.T) {
	recent := make([]string, 0, 25)
	var older string
	for i := 1; i <= 25; i++ {
		entry := "User: turn " + string(rune('A'+i-1))
		recent, older = AppendConversation(recent, older, entry)
	}
	if len(recent) != 25 {
		t.Fatalf("len(recent) = %d, want 25", len(recent))
	}
	if older != "" {
		t.Fatalf("unexpected early conversation summary: %q", older)
	}
	if !strings.Contains(recent[0], "User: turn A") || !strings.Contains(recent[24], "User: turn Y") {
		t.Fatalf("authoritative conversation lost turns: %#v", recent)
	}
}

func TestAppendConversationRetainsLongTurnsUntilProjection(t *testing.T) {
	huge := "User: " + strings.Repeat("a", 12000)
	recent, older := AppendConversation(nil, "", huge)
	for i := 0; i < 6; i++ {
		recent, older = AppendConversation(recent, older, huge)
	}
	if len(recent) != 7 || recent[0] != huge {
		t.Fatalf("authoritative conversation lost a long turn: %d", len(recent))
	}
	if older != "" {
		t.Fatalf("conversation was summarized before budget pressure: %q", older)
	}
}

func TestAppendConversationCapsOlderSummaryNotes(t *testing.T) {
	older := ""
	for i := 0; i < 45; i++ {
		older = CarryConversationSummary(older, []string{"User: prior task note " + strings.Repeat("x", i%3)})
	}
	notes := notesFromSummary(older)
	if len(notes) > olderSummaryNoteLimit {
		t.Fatalf("len(notes) = %d, want <= %d", len(notes), olderSummaryNoteLimit)
	}
}

func TestCarryConversationSummaryAppendsEntriesToOlderSummary(t *testing.T) {
	got := CarryConversationSummary("User: first task", []string{"Assistant: done", "User: next task"})
	for _, want := range []string{"User: first task", "Assistant: done", "User: next task"} {
		if !strings.Contains(got, want) {
			t.Fatalf("carry summary missing %q in %q", want, got)
		}
	}
}

func TestCarryConversationSummaryKeepsLongerEntriesWithoutEarlyTruncation(t *testing.T) {
	long := "Assistant: " + strings.Repeat("detail ", 60)
	got := CarryConversationSummary("", []string{long})
	if strings.Contains(got, "...") {
		t.Fatalf("carry summary truncated too early: %q", got)
	}
	if !strings.Contains(got, "detail detail detail") {
		t.Fatalf("carry summary lost detail content: %q", got)
	}
}
