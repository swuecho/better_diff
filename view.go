package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	headerRows = 2
	footerRows = 1
)

// View implements tea.Model
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	// If help is shown, render help modal overlay
	if m.showHelp {
		return m.renderHelpModal()
	}

	// Calculate dimensions
	contentHeight := m.height - headerRows - footerRows
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
	var parts []string

	if m.branch != "" {
		parts = append(parts, headerStyle.Render("better_diff"))
		parts = append(parts, subtleStyle.Render(m.branch))
	}

	if m.rootPath != "" {
		parts = append(parts, subtleStyle.Render(m.rootPath))
	}

	parts = append(parts, modeIndicatorStyle.Render("["+m.diffModeLabel()+"]"))

	parts = append(parts, viewModeIndicatorStyle.Render("["+m.diffViewModeLabel()+"]"))

	files, added, removed := m.GetTotalStats()
	if files > 0 {
		stats := formatAggregateStats(files, added, removed)
		parts = append(parts, statsSubtleStyle.Render("("+stats+")"))
	}

	if !m.showHelp {
		parts = append(parts, subtleStyle.Render("Press ? for help"))
	}

	header := strings.Join(parts, " ")
	separator := headerSeparatorStyle.Render(strings.Repeat("─", m.width))

	return lipgloss.JoinVertical(lipgloss.Left, header, separator)
}

func (m Model) renderContent(height int) string {
	// In whole file mode, hide the file tree and show only diff
	if m.diffViewMode == WholeFile {
		return m.renderDiffPanel(m.width, height)
	}

	// Split into two panels - left panel gets 1/3, right panel gets 2/3
	leftPanelWidth := m.width / 3
	rightPanelWidth := m.width - leftPanelWidth

	leftPanel := m.renderFileTree(leftPanelWidth, height)
	rightPanel := m.renderDiffPanel(rightPanelWidth, height)

	// Join panels horizontally
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
}

func (m Model) renderFileTree(width, height int) string {
	selectedStyle := fileTreeSelectedLineStyle.Width(max(0, width-2))
	internalHeight := panelContentHeight(height)

	// Get visible nodes
	flatTree := m.flattenTree()
	start, end := visibleRange(m.scrollOffset, internalHeight, len(flatTree))
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

		text := node.name
		style := fileStyle
		if node.isDir {
			style = dirStyle
		}

		// Add change indicator
		var indicator string
		switch node.changeType {
		case Modified:
			indicator = "●"
			if !node.isDir {
				style = modifiedStyle
			}
		case Added:
			indicator = "+"
			if !node.isDir {
				style = addedStyle
			}
		case Deleted:
			indicator = "-"
			if !node.isDir {
				style = deletedStyle
			}
		default:
			indicator = "●"
		}

		line := prefix + indicator + " " + text

		if !node.isDir && (node.linesAdded > 0 || node.linesRemoved > 0) {
			stats := formatLineStats(node.linesAdded, node.linesRemoved)
			line += statsStyle.Render(stats)
		}

		if isSelected && m.panel == FileTreePanel {
			line = selectedStyle.Render(line)
		} else {
			line = style.Render(line)
		}

		lines = append(lines, line)
	}

	// Build content as a single string
	content := strings.Join(lines, "\n")

	// Apply panel styling with border
	return m.renderPanel(content, width, height, m.panel == FileTreePanel)
}

func (m Model) renderDiffPanel(width, height int) string {
	// Get selected file(s).
	filesToRender := m.getSelectedDiffFiles()

	// Render all diff lines first
	var allLines []string

	if m.diffMode == BranchCompare {
		// Add branch compare info header.
		allLines = append(allLines, diffCommitHeaderStyle.Render("Branch Compare: current working tree vs default branch"))
		allLines = append(allLines, "")
	}

	if len(filesToRender) == 0 {
		// No file selected
		msg := "Select a file to view diff"
		if m.diffMode == BranchCompare {
			msg = "Select a file to view unified changes"
		}
		allLines = append(allLines, panelInfoStyle.Render(msg))
	} else {
		for fileIdx, selectedFile := range filesToRender {
			allLines = appendRenderedFileDiffLines(allLines, selectedFile, fileIdx > 0)
		}
	}

	internalHeight := panelContentHeight(height)
	start, end := visibleRange(m.diffScroll, internalHeight, len(allLines))

	var lines []string
	if start < len(allLines) && end > start {
		lines = allLines[start:end]
	}

	// Pad to exact content height
	for len(lines) < internalHeight {
		lines = append(lines, "")
	}

	// Build content as a single string
	content := strings.Join(lines, "\n")

	// Apply panel styling with border
	return m.renderPanel(content, width, height, m.panel == DiffPanel)
}

func appendRenderedFileDiffLines(lines []string, file *FileDiff, withSeparator bool) []string {
	if withSeparator {
		lines = append(lines, "")
		lines = append(lines, diffHunkStyle.Render("════════════════════════════════════"))
	}

	lines = append(lines, diffFileHeaderStyle.Render("📄 "+file.Path))
	if len(file.Hunks) == 0 {
		return append(lines, panelInfoStyle.Render("No diff content available (binary file or no changes)"))
	}

	for _, hunk := range file.Hunks {
		lines = append(lines, diffHunkStyle.Render("─"))
		for _, diffLine := range hunk.Lines {
			lines = append(lines, renderDiffLine(diffLine))
		}
	}
	return lines
}

func renderDiffLine(diffLine DiffLine) string {
	var (
		prefix       string
		prefixStyle  lipgloss.Style
		contentStyle lipgloss.Style
	)

	switch diffLine.Type {
	case LineAdded:
		prefix = "+"
		prefixStyle = diffAddedPrefixStyle
		contentStyle = diffAddedStyle
	case LineRemoved:
		prefix = "-"
		prefixStyle = diffRemovedPrefixStyle
		contentStyle = diffRemovedStyle
	default:
		prefix = " "
		prefixStyle = diffContextStyle
		contentStyle = diffContextStyle
	}

	return prefixStyle.Render(prefix) + " " + contentStyle.Render(diffLine.Content)
}

func (m Model) renderFooter() string {
	// Build contextual help text to reduce footer overload.
	help := []string{}

	if m.panel == FileTreePanel {
		help = append(help, footerKeyStyle.Render("[↑↓/j/k]")+" Navigate")
		help = append(help, footerKeyStyle.Render("[PgUp/PgDn]")+" Page")
		help = append(help, footerKeyStyle.Render("[Enter]")+" Select/Expand")
	} else {
		help = append(help, footerKeyStyle.Render("[↑↓]")+" Scroll")
		help = append(help, footerKeyStyle.Render("[PgUp/PgDn]")+" Page")
		if m.diffViewMode == DiffOnly {
			help = append(help, footerKeyStyle.Render("[j/k]")+" Hunk Jump")
		} else {
			help = append(help, footerKeyStyle.Render("[j/k]")+" Scroll")
		}
		help = append(help, footerKeyStyle.Render("[gg/G]")+" Top/Bottom")
	}

	if m.diffViewMode == DiffOnly {
		help = append(help, footerKeyStyle.Render("[o/O]")+" Expand/Reset")
		help = append(help, footerKeyStyle.Render("[Tab]")+" Switch Panel")
	}

	help = append(help, footerKeyStyle.Render("[s]")+" Mode")
	help = append(help, footerKeyStyle.Render("[f]")+" Diff/Whole File")
	help = append(help, footerKeyStyle.Render("[?]")+" Help")
	help = append(help, footerKeyStyle.Render("[q]")+" Quit")

	// Add scroll indicator for diff panel
	if m.panel == DiffPanel {
		totalLines := m.getDiffLineCount()
		if totalLines > 0 {
			scrollPercent := computeScrollPercent(m.diffScroll, totalLines, m.visibleContentRows())
			help = append(help, footerScrollStyle.Render(fmt.Sprintf("Scroll: %d%%", scrollPercent)))
		}
	}

	if m.err != nil {
		help = append(help, errorStyle.Render("Error: "+m.err.Error()))
	}

	return footerBaseStyle.Render(strings.Join(help, " • "))
}

func computeScrollPercent(scrollPos, totalLines, visibleHeight int) int {
	maxScroll := max(0, totalLines-visibleHeight)
	if maxScroll == 0 {
		return 100
	}
	scrollPos = clamp(scrollPos, 0, maxScroll)
	return (scrollPos * 100) / maxScroll
}

func (m Model) diffModeLabel() string {
	switch m.diffMode {
	case Staged:
		return "Staged"
	case BranchCompare:
		return "Branch Compare"
	default:
		return "Unstaged"
	}
}

func (m Model) diffViewModeLabel() string {
	if m.diffViewMode == WholeFile {
		return "Whole File"
	}
	return "Diff Only"
}

func formatAggregateStats(fileCount, linesAdded, linesRemoved int) string {
	switch {
	case linesAdded > 0 && linesRemoved > 0:
		return fmt.Sprintf("%d files, +%d/-%d", fileCount, linesAdded, linesRemoved)
	case linesAdded > 0:
		return fmt.Sprintf("%d files, +%d", fileCount, linesAdded)
	case linesRemoved > 0:
		return fmt.Sprintf("%d files, -%d", fileCount, linesRemoved)
	default:
		return fmt.Sprintf("%d files", fileCount)
	}
}

func formatLineStats(linesAdded, linesRemoved int) string {
	switch {
	case linesAdded > 0 && linesRemoved > 0:
		return fmt.Sprintf(" +%d/-%d", linesAdded, linesRemoved)
	case linesAdded > 0:
		return fmt.Sprintf(" +%d", linesAdded)
	default:
		return fmt.Sprintf(" -%d", linesRemoved)
	}
}

// renderHelpModal renders the help modal overlay
func (m Model) renderHelpModal() string {
	return m.renderHelp()
}

// renderCommits renders the list of commits for branch comparison
func (m Model) renderCommits(width, height int) string {
	selectedStyle := fileTreeSelectedLineStyle.Width(max(0, width-2))
	internalHeight := panelContentHeight(height)

	// If no commits ahead, show changes summary
	if len(m.commits) == 0 {
		var lines []string
		lines = append(lines, panelInfoStyle.Render("No commits ahead of main"))

		// Show changed file count if available.
		if len(m.diffFiles) > 0 {
			lines = append(lines, "")
			lines = append(lines, panelInfoStyle.Render(fmt.Sprintf("Changes: %d files", len(m.diffFiles))))
		}

		content := strings.Join(lines, "\n")

		return m.renderPanel(content, width, height, m.panel == FileTreePanel)
	}

	// Get visible commits.
	start, end := visibleRange(m.scrollOffset, internalHeight, len(m.commits))
	visibleCommits := m.commits[start:end]

	// Render each commit
	var lines []string
	for i, commit := range visibleCommits {
		globalIndex := start + i
		isSelected := globalIndex == m.selectedIndex

		// Build commit line
		line := commitHashStyle.Render(commit.ShortHash) + " " +
			commitAuthorStyle.Render(commit.Author) + " " +
			commitDateStyle.Render(commit.Date) + "\n" +
			"    " + commitMessageStyle.Render(commit.Message)

		if isSelected && m.panel == FileTreePanel {
			line = selectedStyle.Render(commit.ShortHash + " " + commit.Author + " " + commit.Date)
			lines = append(lines, line)
			// Add message on next line
			lines = append(lines, "    "+commitMessageStyle.Render(commit.Message))
		} else {
			lines = append(lines, line)
		}
	}

	// Build content as a single string
	content := strings.Join(lines, "\n")

	// Apply panel styling with border
	return m.renderPanel(content, width, height, m.panel == FileTreePanel)
}

func (m Model) renderPanel(content string, width, height int, active bool) string {
	style := panelBaseStyle
	if active {
		style = panelActiveStyle
	}

	return style.
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		Render(content)
}
