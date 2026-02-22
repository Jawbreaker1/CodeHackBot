package main

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/orchestrator"
)

func appendLogLines(lines []string, text string) []string {
	chunks := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := append([]string{}, lines...)
	for _, chunk := range chunks {
		cleaned := sanitizeLogLine(chunk)
		if cleaned == "" {
			continue
		}
		out = append(out, cleaned)
	}
	if len(out) > tuiCommandLogStoreLines {
		return out[len(out)-tuiCommandLogStoreLines:]
	}
	return out
}

func sanitizeLogLine(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if r < 32 && r != '\t' {
			continue
		}
		if r == '\t' {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func renderCommandLog(messages []string, wrapWidth int, scroll *int) []string {
	if wrapWidth < 24 {
		wrapWidth = 24
	}
	rendered := make([]string, 0, len(messages)*2)
	for _, msg := range messages {
		parts := wrapText(msg, wrapWidth)
		if len(parts) == 0 {
			continue
		}
		for i, part := range parts {
			if i == 0 {
				rendered = append(rendered, "  - "+part)
			} else {
				rendered = append(rendered, "    "+part)
			}
		}
	}
	if len(rendered) == 0 {
		rendered = append(rendered, "  (empty)")
	}
	maxScroll := 0
	if len(rendered) > tuiCommandLogViewLines {
		maxScroll = len(rendered) - tuiCommandLogViewLines
	}
	offset := 0
	if scroll != nil {
		offset = *scroll
	}
	if offset < 0 {
		offset = 0
	}
	if offset > maxScroll {
		offset = maxScroll
	}
	if scroll != nil {
		*scroll = offset
	}
	end := len(rendered) - offset
	start := end - tuiCommandLogViewLines
	if start < 0 {
		start = 0
	}
	view := append([]string{}, rendered[start:end]...)
	if start > 0 {
		view = append([]string{fmt.Sprintf("  ... %d older line(s) (up/down to scroll)", start)}, view...)
	}
	if end < len(rendered) {
		view = append(view, fmt.Sprintf("  ... %d newer line(s)", len(rendered)-end))
	}
	return view
}

func renderRecentEvents(events []orchestrator.EventEnvelope, scroll *int) []string {
	rendered := make([]string, 0, len(events))
	for _, event := range events {
		rendered = append(rendered, fmt.Sprintf("  - %s | %s | worker=%s | task=%s", event.TS.Format("15:04:05"), event.Type, event.WorkerID, event.TaskID))
	}
	if len(rendered) == 0 {
		rendered = append(rendered, "  (no events)")
	}
	maxScroll := 0
	if len(rendered) > tuiRecentEventsView {
		maxScroll = len(rendered) - tuiRecentEventsView
	}
	offset := 0
	if scroll != nil {
		offset = *scroll
	}
	if offset < 0 {
		offset = 0
	}
	if offset > maxScroll {
		offset = maxScroll
	}
	if scroll != nil {
		*scroll = offset
	}
	end := len(rendered) - offset
	start := end - tuiRecentEventsView
	if start < 0 {
		start = 0
	}
	view := append([]string{}, rendered[start:end]...)
	if start > 0 {
		view = append([]string{fmt.Sprintf("  ... %d older event(s) (events up/down to scroll)", start)}, view...)
	}
	if end < len(rendered) {
		view = append(view, fmt.Sprintf("  ... %d newer event(s)", len(rendered)-end))
	}
	return view
}

func wrapText(text string, width int) []string {
	cleaned := sanitizeLogLine(text)
	if cleaned == "" {
		return nil
	}
	if width <= 0 {
		return []string{cleaned}
	}
	words := strings.Fields(cleaned)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 2)
	current := words[0]
	for _, word := range words[1:] {
		if len([]rune(current))+1+len([]rune(word)) <= width {
			current += " " + word
			continue
		}
		lines = append(lines, current)
		if len([]rune(word)) > width {
			runes := []rune(word)
			for len(runes) > width {
				lines = append(lines, string(runes[:width]))
				runes = runes[width:]
			}
			current = string(runes)
			continue
		}
		current = word
	}
	if strings.TrimSpace(current) != "" {
		lines = append(lines, current)
	}
	return lines
}

func clampTUIBodyLines(lines []string, maxBody int) []string {
	if maxBody <= 0 {
		return nil
	}
	if len(lines) <= maxBody {
		return lines
	}
	const headKeep = 10
	head := headKeep
	if head > maxBody {
		head = maxBody
	}
	tail := maxBody - head
	if tail <= 0 {
		return append([]string{}, lines[:head]...)
	}
	out := make([]string, 0, maxBody)
	out = append(out, lines[:head]...)
	out = append(out, lines[len(lines)-tail:]...)
	return out
}
