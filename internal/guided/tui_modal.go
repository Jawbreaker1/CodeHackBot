package guided

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
)

func (m guidedTUI) viewModal(base string) string {
	contentWidth := min(62, m.width-12)
	if contentWidth < 30 {
		contentWidth = 30
	}
	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	var rows []string
	rows = append(rows, accent.Render(m.modalTitle), "")
	if m.modalHeading != "" {
		rows = append(rows, lipgloss.NewStyle().Bold(true).Render(m.modalHeading), "")
	}
	if len(m.modalChoices) > 0 {
		visible := maxTUI(3, (m.height-11)/2)
		start := 0
		if m.modalSelected >= visible {
			start = m.modalSelected - visible + 1
		}
		end := min(len(m.modalChoices), start+visible)
		if start > 0 {
			rows = append(rows, muted.Render("↑ more choices"))
		}
		for i := start; i < end; i++ {
			choice := m.modalChoices[i]
			marker := "  "
			label := choice.Label
			if i == m.modalSelected {
				marker = "› "
				label = accent.Render(label)
			}
			rows = append(rows, marker+label)
			if choice.Detail != "" {
				rows = append(rows, muted.Render("   "+choice.Detail))
			}
		}
		if end < len(m.modalChoices) {
			rows = append(rows, muted.Render("↓ more choices"))
		}
		rows = append(rows, "", muted.Render("↑/↓ select  ·  Enter apply  ·  Esc close"))
	} else {
		var details []string
		for _, text := range m.modalLines {
			for _, line := range strings.Split(text, "\n") {
				if strings.TrimSpace(line) != "" {
					details = append(details, line)
				}
			}
		}
		if len(details) > 6 {
			details = details[len(details)-6:]
		}
		for _, line := range details {
			rows = append(rows, muted.Render(line))
		}
		if m.waiting {
			rows = append(rows, "", m.inputPrompt, m.input.View(), "", muted.Render("Enter continue  ·  Esc close"))
		} else {
			rows = append(rows, "", muted.Render("Applying…"))
		}
	}
	modal := lipgloss.NewStyle().Width(contentWidth).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("203")).Padding(1, 2).Render(strings.Join(rows, "\n"))
	modalWidth, modalHeight := lipgloss.Width(modal), lipgloss.Height(modal)
	x := maxTUI(0, (m.width-modalWidth)/2)
	y := maxTUI(0, (m.height-modalHeight)/2)
	buffer := cellbuf.NewBuffer(m.width, m.height)
	cellbuf.SetContent(buffer, base)
	cellbuf.SetContentRect(buffer, modal, cellbuf.Rect(x, y, modalWidth, modalHeight))
	return strings.ReplaceAll(cellbuf.Render(buffer), "\r\n", "\n")
}
