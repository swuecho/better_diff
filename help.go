package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// KeyBinding represents a keyboard shortcut with its description
type KeyBinding struct {
	Key     string
	Action  string
	Section string
}

// All key bindings for the application
var keyBindings = []KeyBinding{
	// Navigation
	{"up/k", "Move up", "Navigation"},
	{"down/j", "Move down", "Navigation"},
	{"pgup", "Page up", "Navigation"},
	{"pgdown", "Page down", "Navigation"},
	{"j/k", "Jump between hunks (Diff Only, diff panel)", "Navigation"},
	{"gg", "Jump to top (diff/whole file)", "Navigation"},
	{"G", "Jump to bottom (diff/whole file)", "Navigation"},
	{"o", "Expand surrounding context (Diff Only)", "Navigation"},
	{"O", "Reset surrounding context (Diff Only)", "Navigation"},

	// Actions
	{"enter/space", "Select file / Expand directory", "Actions"},
	{"s", "Toggle unstaged/staged/branch compare", "Actions"},
	{"f", "Toggle diff/whole file view", "Actions"},

	// Panels
	{"tab", "Switch between file tree and diff", "Panels"},

	// Quit
	{"q/ctrl+c", "Quit application", "System"},

	// Help
	{"?", "Show/hide this help screen", "System"},
}

// ShowHelp shows the help modal
func (m *Model) ShowHelp() tea.Cmd {
	return func() tea.Msg {
		return ShowHelpMsg{}
	}
}

// HideHelp hides the help modal
func (m *Model) HideHelp() tea.Cmd {
	return func() tea.Msg {
		return HideHelpMsg{}
	}
}

// ShowHelpMsg is a message to show the help modal
type ShowHelpMsg struct{}

// HideHelpMsg is a message to hide the help modal
type HideHelpMsg struct{}

// renderHelp renders the help modal
func (m Model) renderHelp() string {
	if !m.showHelp {
		return ""
	}

	// Calculate modal dimensions
	modalWidth := min(60, m.width-4)
	modalHeight := min(30, m.height-4)

	// Create modal style
	modalStyle := lipgloss.NewStyle().
		Width(modalWidth).
		Height(modalHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBlue).
		Background(colorGray235).
		Padding(1, 2)

	// Build help content
	var content strings.Builder

	// Title
	title := helpTitleStyle.Render("Keyboard Shortcuts")
	content.WriteString(title)
	content.WriteString("\n\n")

	// Group bindings by section
	currentSection := ""
	for _, kb := range keyBindings {
		if kb.Section != currentSection {
			currentSection = kb.Section
			content.WriteString("\n")
			content.WriteString(helpSectionStyle.Render(currentSection))
			content.WriteString("\n")
		}

		// Render key and description
		key := helpKeyStyle.Render(fmt.Sprintf(" %-8s", kb.Key))
		desc := helpDescStyle.Render(kb.Action)
		content.WriteString(fmt.Sprintf("%s %s\n", key, desc))
	}

	// Footer
	content.WriteString("\n")
	footer := subtleStyle.Render("Press ? to close")
	content.WriteString(footer)

	// Center the modal
	helpContent := modalStyle.Render(content.String())
	helpLines := strings.Split(helpContent, "\n")

	// Center vertically and horizontally
	verticalPadding := (m.height - len(helpLines)) / 2
	if verticalPadding < 0 {
		verticalPadding = 0
	}

	horizontalPadding := (m.width - modalWidth) / 2
	if horizontalPadding < 0 {
		horizontalPadding = 0
	}

	// Build centered help
	var result strings.Builder
	for i := 0; i < verticalPadding; i++ {
		result.WriteString("\n")
	}

	for _, line := range helpLines {
		for i := 0; i < horizontalPadding; i++ {
			result.WriteString(" ")
		}
		result.WriteString(line)
		result.WriteString("\n")
	}

	return result.String()
}

// GetKeyBindings returns all key bindings (for documentation/testing)
func GetKeyBindings() []KeyBinding {
	return keyBindings
}
