package guided

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

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
	color := lipgloss.Color("252")
	if label == "You" {
		color = lipgloss.Color("203")
	}
	if label == "Coordinator plan" {
		label = "Plan"
		color = lipgloss.Color("81")
	}
	return lipgloss.NewStyle().Bold(true).Foreground(color).Render(label)
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
