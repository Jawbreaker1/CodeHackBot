package context

import (
	"strings"
)

const (
	olderSummaryNoteLimit    = 40
	olderSummaryNoteMaxChars = 800
)

func AppendConversation(recent []string, olderSummary, entry string) ([]string, string) {
	entry = normalizeConversationEntry(entry)
	if entry == "" {
		return recent, olderSummary
	}

	// The saved worker packet retains exact turns. ModelView, not this
	// authoritative state, decides when a particular request needs pruning.
	return append(append([]string{}, recent...), entry), olderSummary
}

func CarryConversationSummary(olderSummary string, entries []string) string {
	return appendOlderConversationSummary(olderSummary, entries)
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
	return strings.TrimSpace(entry)
}
