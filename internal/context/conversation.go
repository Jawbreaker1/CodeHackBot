package context

import (
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/tokenutil"
)

const (
	recentConversationTurnLimit  = 20
	recentConversationTokenLimit = 20000
	olderSummaryNoteLimit        = 20
	olderSummaryNoteMaxChars     = 220
)

func AppendConversation(recent []string, olderSummary, entry string) ([]string, string) {
	entry = normalizeConversationEntry(entry)
	if entry == "" {
		return recent, olderSummary
	}

	next := append(append([]string{}, recent...), entry)
	overflow := make([]string, 0)
	for len(next) > recentConversationTurnLimit || approxConversationTokens(next) > recentConversationTokenLimit {
		if len(next) == 0 {
			break
		}
		overflow = append(overflow, next[0])
		next = next[1:]
	}
	if len(overflow) == 0 {
		return next, olderSummary
	}
	return next, appendOlderConversationSummary(olderSummary, overflow)
}

func approxConversationTokens(turns []string) int {
	if len(turns) == 0 {
		return 0
	}
	return tokenutil.ApproxTokens(strings.Join(turns, "\n"))
}

func appendOlderConversationSummary(existing string, overflow []string) string {
	notes := notesFromSummary(existing)
	for _, entry := range overflow {
		entry = compactConversationEntry(entry, olderSummaryNoteMaxChars)
		if entry == "" {
			continue
		}
		notes = append(notes, entry)
	}
	if len(notes) > olderSummaryNoteLimit {
		notes = notes[len(notes)-olderSummaryNoteLimit:]
	}
	return strings.Join(notes, " | ")
}

func notesFromSummary(summary string) []string {
	summary = strings.TrimSpace(summary)
	if summary == "" || summary == "(none)" {
		return nil
	}
	parts := strings.Split(summary, " | ")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = normalizeConversationEntry(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func compactConversationEntry(entry string, max int) string {
	entry = normalizeConversationEntry(entry)
	if entry == "" || max <= 0 || len(entry) <= max {
		return entry
	}
	if max <= 3 {
		return entry[:max]
	}
	return strings.TrimSpace(entry[:max-3]) + "..."
}

func normalizeConversationEntry(entry string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(entry)), " ")
}
