package guided

import (
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	tuiAccent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	tuiBlue   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
	tuiGreen  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("114"))
	tuiMuted  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	tuiBody   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)

func tuiSectionTitle(text string) string { return tuiGreen.Render(text) }

// statusBody gives the operator a short, live overview. The full worker
// snapshot remains available through /workers when the terminal is small.
func (m guidedTUI) statusBody(width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	phase := "SETUP"
	phaseStyle := tuiBlue
	if m.active {
		phase = "RUNNING"
		phaseStyle = tuiGreen
	}
	if m.sessionReady {
		phase = "READY TO CONTINUE"
		phaseStyle = tuiBlue
	}
	if m.stopping {
		phase = "STOPPING"
		phaseStyle = tuiAccent
	}
	gap := []string{""}
	if height <= 12 {
		gap = nil
	}
	rows := []string{phaseStyle.Render(phase)}
	rows = append(rows, gap...)
	rows = append(rows, tuiMuted.Render("COORDINATOR"))
	coordinator := strings.TrimSpace(m.coordinatorStatus)
	if coordinator == "" {
		coordinator = "ready"
	}
	if m.coordinatorWorking {
		coordinator = m.spinner.View() + " " + coordinator
	}
	rows = append(rows, tuiBody.Render(ansi.Truncate(coordinator, width, "…")))
	rows = append(rows, gap...)
	rows = append(rows, tuiMuted.Render("APPROVALS"))
	permission := m.permissionLabel
	if permission == "" {
		permission = approval.EveryExecution.Label()
	}
	for _, line := range strings.Split(ansi.Wrap(permission, width, ""), "\n") {
		rows = append(rows, tuiBody.Render(line))
	}
	if notice := strings.TrimSpace(m.notice); notice != "" {
		rows = append(rows, tuiMuted.Render(ansi.Truncate(notice, width, "…")))
	}
	workersTitle := fmt.Sprintf("WORKERS · %d ACTIVE", m.activeWorkers)
	if m.activeWorkers > 0 {
		workersTitle = m.spinner.View() + " " + workersTitle
	}
	rows = append(rows, gap...)
	rows = append(rows, tuiGreen.Render(workersTitle))
	remaining := height - len(rows)
	for _, line := range tuiWorkerRows(m.workerStatus, remaining) {
		if len(rows) >= height {
			break
		}
		style := tuiBlue
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "…") || line == "No workers yet" {
			style = tuiMuted
		}
		rows = append(rows, style.Render(ansi.Truncate(line, width, "…")))
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	return strings.Join(rows, "\n")
}

// compactSnapshot uses unindented task names followed by indented details.
// Keep the names visible first when a narrow inspector cannot show everything.
func tuiWorkerRows(snapshot string, available int) []string {
	if available <= 0 {
		return nil
	}
	if strings.TrimSpace(snapshot) == "" {
		return []string{"No workers yet"}
	}
	type workerGroup struct {
		name    string
		details []string
	}
	var groups []workerGroup
	for _, line := range strings.Split(snapshot, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, " ") && len(groups) > 0 {
			groups[len(groups)-1].details = append(groups[len(groups)-1].details, line)
		} else {
			groups = append(groups, workerGroup{name: line})
		}
	}
	if len(groups) > available {
		var rows []string
		for _, group := range groups[:available-1] {
			rows = append(rows, group.name)
		}
		return append(rows, "… see /workers")
	}
	var rows []string
	extra := available - len(groups)
	for i, group := range groups {
		rows = append(rows, group.name)
		share := extra / (len(groups) - i)
		if share > len(group.details) {
			share = len(group.details)
		}
		rows = append(rows, group.details[:share]...)
		extra -= share
	}
	return rows
}

// The terminal renders presentation markup only. It never interprets model
// output as commands or changes the assessment's saved conversation.
func renderTUIConversation(entries []string, width int) string {
	if width < 1 {
		return ""
	}
	var blocks []string
	for _, entry := range entries {
		entry = strings.Trim(entry, "\n")
		if strings.TrimSpace(entry) == "" {
			continue
		}
		if entry == cliLogo {
			blocks = append(blocks, entry)
			continue
		}
		label, body := tuiMessageParts(entry)
		if label != "" {
			blocks = append(blocks, tuiLabel(label)+"\n"+renderTUIMarkdown(body, width))
		} else {
			blocks = append(blocks, renderTUIMarkdown(entry, width))
		}
	}
	return strings.Join(blocks, "\n\n")
}

func tuiMessageParts(entry string) (string, string) {
	for _, label := range []string{"Coordinator plan", "Coordinator", "You", "Application"} {
		if body, ok := strings.CutPrefix(entry, label+": "); ok {
			return label, body
		}
	}
	return "", entry
}

func tuiLabel(label string) string {
	switch label {
	case "You":
		return tuiAccent.Render("YOU")
	case "Coordinator plan":
		return tuiGreen.Render("PLAN")
	case "Coordinator":
		return tuiBlue.Render("COORDINATOR")
	default:
		return tuiMuted.Render("APPLICATION")
	}
}

func renderTUIMarkdown(body string, width int) string {
	var lines []string
	inCode := false
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	for _, raw := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if trimmed == "" {
			lines = append(lines, "")
			continue
		}
		if inCode {
			lines = append(lines, ansi.Hardwrap(muted.Render("  "+raw), width, true))
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if title := strings.TrimLeft(trimmed, "#"); strings.HasPrefix(title, " ") {
				lines = append(lines, ansi.Wrap(heading.Render(strings.TrimSpace(title)), width, ""))
				continue
			}
		}
		lines = append(lines, ansi.Wrap(tuiInlineMarkdown(raw), width, ""))
	}
	return strings.Join(lines, "\n")
}

func tuiInlineMarkdown(line string) string {
	bold := lipgloss.NewStyle().Bold(true)
	code := lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	var out strings.Builder
	for len(line) > 0 {
		marker := strings.IndexAny(line, "*`\n")
		if marker < 0 {
			out.WriteString(line)
			break
		}
		out.WriteString(line[:marker])
		line = line[marker:]
		if strings.HasPrefix(line, "**") {
			if end := strings.Index(line[2:], "**"); end >= 0 {
				out.WriteString(bold.Render(line[2 : end+2]))
				line = line[end+4:]
				continue
			}
		}
		if strings.HasPrefix(line, "`") {
			if end := strings.IndexByte(line[1:], '`'); end >= 0 {
				out.WriteString(code.Render(line[1 : end+1]))
				line = line[end+2:]
				continue
			}
		}
		out.WriteByte(line[0])
		line = line[1:]
	}
	return out.String()
}
