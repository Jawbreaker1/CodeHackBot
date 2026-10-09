package guided

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type slashCommand struct {
	name      string
	shorthint string
}

var intakeSlashCommands = []slashCommand{
	{"/settings", "Model"},
	{"/permissions", "Approvals"},
	{"/resume", "Saved session"},
	{"/help", "Help"},
	{"/exit", "Exit"},
}

var runningSlashCommands = []slashCommand{
	{"/plan", "Current plan"},
	{"/workers", "Workers"},
	{"/findings", "Findings"},
	{"/artifacts", "Artifacts"},
	{"/status", "Progress"},
	{"/permissions", "Approvals"},
	{"/settings", "Model"},
	{"/help", "Help"},
	{"/stop", "Stop run"},
}

var finishedSlashCommands = []slashCommand{
	{"/plan", "Latest plan"},
	{"/workers", "Workers"},
	{"/findings", "Findings"},
	{"/artifacts", "Artifacts"},
	{"/status", "Run status"},
	{"/permissions", "Approvals"},
	{"/settings", "Model"},
	{"/help", "Help"},
	{"/exit", "Exit"},
}

func (m guidedTUI) slashMatches() []slashCommand {
	if m.busy || m.modalActive || (m.waiting && m.inputPrompt != "birdhackbot>") {
		return nil
	}
	value := m.input.Value()
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, " \t") {
		return nil
	}
	commands := intakeSlashCommands
	if m.active {
		commands = runningSlashCommands
		if m.sessionReady {
			commands = finishedSlashCommands
		}
	}
	var matches []slashCommand
	for _, command := range commands {
		if strings.HasPrefix(command.name, strings.ToLower(value)) {
			matches = append(matches, command)
		}
	}
	return matches
}

func (m guidedTUI) slashPaletteLines() []string {
	matches := m.slashMatches()
	if len(matches) == 0 {
		return nil
	}
	selected := m.slashSelected
	if selected >= len(matches) {
		selected = 0
	}
	lines := []string{"Slash commands · ↑/↓ choose · Tab complete · Enter run"}
	columnWidth := maxTUI(20, (m.width-9)/2)
	for row := 0; row < (len(matches)+1)/2; row++ {
		left := slashCommandLabel(matches[row*2], row*2 == selected)
		line := lipgloss.NewStyle().Width(columnWidth).Render(left)
		if rightIndex := row*2 + 1; rightIndex < len(matches) {
			line += "  " + slashCommandLabel(matches[rightIndex], rightIndex == selected)
		}
		lines = append(lines, line)
	}
	return lines
}

func slashCommandLabel(command slashCommand, selected bool) string {
	marker := "  "
	if selected {
		marker = "› "
	}
	label := marker + command.name + "  " + command.shorthint
	if selected {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Render(label)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(label)
}
