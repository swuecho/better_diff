package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View implements tea.Model
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	// Calculate dimensions
	headerHeight := 2
	footerHeight := 1
	contentHeight := m.height - headerHeight - footerHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Create header
	header := m.renderHeader()

	// Create main content (split view)
	content := m.renderContent(contentHeight)

	// Create footer
	footer := m.renderFooter()

	// Combine all parts
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

func (m Model) renderHeader() string {
	// Styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("blue"))

	pathStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")) // gray

	// Build header
	var parts []string

	if m.branch != "" {
		parts = append(parts, headerStyle.Render("better_diff"))
		parts = append(parts, pathStyle.Render(m.branch))
	}

	if m.rootPath != "" {
		parts = append(parts, pathStyle.Render(m.rootPath))
	}

	modeText := "Unstaged"
	if m.diffMode == Staged {
		modeText = "Staged"
	}
	modeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("yellow")).
		Bold(true)
	parts = append(parts, modeStyle.Render("["+modeText+"]"))

	// Join with spacing
	header := strings.Join(parts, " ")

	// Add separator
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("237")).
		Render(strings.Repeat("─", m.width))

	return lipgloss.JoinVertical(lipgloss.Left, header, separator)
}

func (m Model) renderContent(height int) string {
	// Split into two panels
	panelWidth := m.width / 2

	leftPanel := m.renderFileTree(panelWidth, height)
	rightPanel := m.renderDiffPanel(m.width-panelWidth, height)

	// Join panels horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
}

func (m Model) renderFileTree(width, height int) string {
	// Styles
	dirStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("blue")).
		Bold(true)

	fileStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("white"))

	modifiedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("yellow"))

	addedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("green"))

	deletedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("red"))

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Width(width - 2)

	// Get visible nodes
	flatTree := m.flattenTree()
	start := m.scrollOffset
	end := min(start+height, len(flatTree))
	if start > len(flatTree) {
		start = len(flatTree)
	}
	if end > len(flatTree) {
		end = len(flatTree)
	}
	visibleNodes := flatTree[start:end]

	// Render each node
	var lines []string
	for i, node := range visibleNodes {
		globalIndex := start + i
		isSelected := globalIndex == m.selectedIndex

		// Build prefix (indentation)
		prefix := strings.Repeat("  ", node.depth)

		// Add folder/file indicator
		if node.isDir {
			if node.isExpanded {
				prefix += "▼ "
			} else {
				prefix += "▶ "
			}
		} else {
			prefix += "  "
		}

		// Style based on type
		var text string
		var style lipgloss.Style

		if node.isDir {
			text = node.name
			style = dirStyle
		} else {
			text = node.name
			style = fileStyle
		}

		// Add change indicator
		var indicator string
		switch node.changeType {
		case Modified:
			indicator = "●" // yellow dot
			if !node.isDir {
				style = modifiedStyle
			}
		case Added:
			indicator = "+" // green plus
			if !node.isDir {
				style = addedStyle
			}
		case Deleted:
			indicator = "-" // red minus
			if !node.isDir {
				style = deletedStyle
			}
		default:
			indicator = "●"
		}

		line := prefix + indicator + " " + text

		if isSelected && m.panel == FileTreePanel {
			line = selectedStyle.Render(line)
		} else {
			line = style.Render(line)
		}

		lines = append(lines, line)
	}

	// Pad to height
	for len(lines) < height {
		lines = append(lines, "")
	}

	// Create panel border
	panelStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("237"))

	if m.panel == FileTreePanel {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("blue"))
	}

	content := strings.Join(lines, "\n")
	return panelStyle.Render(content)
}

func (m Model) renderDiffPanel(width, height int) string {
	// Styles
	addedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("green"))

	removedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("red"))

	contextStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	// Get selected file
	var selectedFile *FileDiff
	flatTree := m.flattenTree()
	if m.selectedIndex < len(flatTree) {
		node := flatTree[m.selectedIndex]
		if !node.isDir {
			for i := range m.diffFiles {
				if m.diffFiles[i].Path == node.path {
					selectedFile = &m.diffFiles[i]
					break
				}
			}
		}
	}

	// Render diff
	var lines []string
	if selectedFile != nil {
		for _, hunk := range selectedFile.Hunks {
			// Add hunk header
			lines = append(lines, contextStyle.Render("..."))

			for _, diffLine := range hunk.Lines {
				var prefix string
				var style lipgloss.Style

				switch diffLine.Type {
				case LineAdded:
					prefix = "+"
					style = addedStyle
				case LineRemoved:
					prefix = "-"
					style = removedStyle
				default:
					prefix = " "
					style = contextStyle
				}

				line := prefix + " " + diffLine.Content
				lines = append(lines, style.Render(line))
			}
		}
	} else {
		// No file selected
		lines = append(lines, lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true).
			Render("Select a file to view diff"))
	}

	// Pad to height
	for len(lines) < height {
		lines = append(lines, "")
	}

	// Create panel
	panelStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("237"))

	if m.panel == DiffPanel {
		panelStyle = panelStyle.BorderForeground(lipgloss.Color("blue"))
	}

	content := strings.Join(lines, "\n")
	return panelStyle.Render(content)
}

func (m Model) renderFooter() string {
	// Styles
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("blue")).
		Bold(true)

	// Build help text
	help := []string{
		keyStyle.Render("[↑↓]") + " Navigate",
		keyStyle.Render("[Enter]") + " Select/Expand",
		keyStyle.Render("[Tab]") + " Switch Panel",
		keyStyle.Render("[s]") + " Staged/Unstaged",
		keyStyle.Render("[q]") + " Quit",
	}

	return footerStyle.Render(strings.Join(help, " • "))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
