package context

import (
	"strings"
	"testing"
)

func TestAppendConversationKeepsRecentTurnsWithinLimit(t *testing.T) {
	recent := make([]string, 0, 25)
	var older string
	for i := 1; i <= 25; i++ {
		entry := "User: turn " + string(rune('A'+i-1))
		recent, older = AppendConversation(recent, older, entry)
	}
	if len(recent) != recentConversationTurnLimit {
		t.Fatalf("len(recent) = %d, want %d", len(recent), recentConversationTurnLimit)
	}
	if !strings.Contains(older, "User: turn A") || !strings.Contains(older, "User: turn E") {
		t.Fatalf("older summary missing overflow turns: %q", older)
	}
	if strings.Contains(strings.Join(recent, " | "), "User: turn A") {
		t.Fatalf("oldest turn still in recent: %#v", recent)
	}
}

func TestAppendConversationRollsOverflowOnTokenLimit(t *testing.T) {
	huge := "User: " + strings.Repeat("a", 12000)
	recent, older := AppendConversation(nil, "", huge)
	for i := 0; i < 6; i++ {
		recent, older = AppendConversation(recent, older, huge)
	}
	if len(recent) >= 7 {
		t.Fatalf("len(recent) = %d, want rollover after token limit", len(recent))
	}
	if older == "" {
		t.Fatalf("older summary empty after token rollover")
	}
}

func TestAppendConversationCapsOlderSummaryNotes(t *testing.T) {
	recent := []string{}
	older := ""
	for i := 0; i < 30; i++ {
		recent, older = AppendConversation(recent, older, "User: overflow note "+strings.Repeat("x", i%3))
	}
	notes := notesFromSummary(older)
	if len(notes) > olderSummaryNoteLimit {
		t.Fatalf("len(notes) = %d, want <= %d", len(notes), olderSummaryNoteLimit)
	}
}
