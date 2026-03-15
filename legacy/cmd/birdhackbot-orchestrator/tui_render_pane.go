package main

import (
	"fmt"
	"strings"
)

type tuiPaneLayout struct {
	enabled    bool
	leftWidth  int
	rightWidth int
	gap        int
}

func computePaneLayout(width int) tuiPaneLayout {
	contentWidth := width - 1
	if contentWidth < 100 {
		return tuiPaneLayout{}
	}
	layout := tuiPaneLayout{
		enabled: true,
		gap:     2,
	}
	layout.rightWidth = contentWidth / 3
	if layout.rightWidth < 38 {
		layout.rightWidth = 38
	}
	if layout.rightWidth > 68 {
		layout.rightWidth = 68
	}
	layout.leftWidth = contentWidth - layout.gap - layout.rightWidth
	if layout.leftWidth < 52 {
		layout.leftWidth = 52
		layout.rightWidth = contentWidth - layout.gap - layout.leftWidth
	}
	if layout.rightWidth < 32 {
		return tuiPaneLayout{}
	}
	return layout
}

func renderTwoPane(left []string, right []string, width int, layout tuiPaneLayout) []string {
	if !layout.enabled || len(right) == 0 {
		out := make([]string, 0, len(left)+len(right)+1)
		for _, line := range left {
			out = append(out, fitLine(line, width))
		}
		if len(right) > 0 {
			out = append(out, fitLine("", width))
			for _, line := range right {
				out = append(out, fitLine(line, width))
			}
		}
		return out
	}
	rows := len(left)
	if rows == 0 {
		rows = len(right)
	}
	if rows == 0 {
		return nil
	}
	if len(right) > rows {
		right = truncatePaneLines(right, rows, "worker lines")
	}
	gap := strings.Repeat(" ", layout.gap)
	out := make([]string, 0, rows)
	for i := 0; i < rows; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, fitColumn(l, layout.leftWidth)+gap+fitColumn(r, layout.rightWidth))
	}
	return out
}

func truncatePaneLines(lines []string, maxRows int, label string) []string {
	if maxRows <= 0 || len(lines) <= maxRows {
		return lines
	}
	trimmed := append([]string{}, lines[:maxRows]...)
	hidden := len(lines) - maxRows + 1
	if hidden < 1 {
		hidden = 1
	}
	suffix := "more"
	if strings.TrimSpace(label) != "" {
		suffix = label
	}
	trimmed[maxRows-1] = fmt.Sprintf("... %d %s", hidden, suffix)
	return trimmed
}

func fitColumn(line string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(line)
	if len(runes) > width {
		if width > 3 {
			return string(runes[:width-3]) + "..."
		}
		return string(runes[:width])
	}
	if len(runes) < width {
		return string(runes) + strings.Repeat(" ", width-len(runes))
	}
	return string(runes)
}
