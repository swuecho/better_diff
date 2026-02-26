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
	// Build header
	var parts []string

	if m.branch != "" {
		parts = append(parts, headerStyle.Render("better_diff"))
		parts = append(parts, subtleStyle.Render(m.branch))
	}

	if m.rootPath != "" {
		parts = append(parts, subtleStyle.Render(m.rootPath))
	}

	modeText := "Unstaged"
	if m.diffMode == Staged {
		modeText = "Staged"
	} else if m.diffMode == BranchCompare {
		modeText = "Branch Compare"
	}
	parts = append(parts, modeIndicatorStyle.Render("["+modeText+"]"))

	// Add view mode indicator
	viewModeText := "Diff Only"
	if m.diffViewMode == WholeFile {
		viewModeText = "Whole File"
	}
	parts = append(parts, viewModeIndicatorStyle.Render("["+viewModeText+"]"))

	// Add summary statistics
	files, added, removed := m.GetTotalStats()
	if files > 0 {
		var stats string
		if added > 0 && removed > 0 {
			stats = fmt.Sprintf("%d files, +%d/-%d", files, added, removed)
		} else if added > 0 {
			stats = fmt.Sprintf("%d files, +%d", files, added)
		} else if removed > 0 {
			stats = fmt.Sprintf("%d files, -%d", files, removed)
		} else {
			stats = fmt.Sprintf("%d files", files)
		}
		parts = append(parts, statsSubtleStyle.Render("("+stats+")"))
	}

	// Add help hint
	if !m.showHelp {
		parts = append(parts, subtleStyle.Render("Press ? for help"))
	}

	// Join with spacing
	header := strings.Join(parts, " ")

	// Add separator
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

	// Calculate internal content height
	internalHeight := height - 2
	if internalHeight < 0 {
		internalHeight = 0
	}

	// Get visible nodes
	flatTree := m.flattenTree()
	start := m.scrollOffset
	end := min(start+internalHeight, len(flatTree))
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

		// Add line statistics for non-dir nodes
		if !node.isDir && (node.linesAdded > 0 || node.linesRemoved > 0) {
			stats := ""
			if node.linesAdded > 0 && node.linesRemoved > 0 {
				stats = fmt.Sprintf(" +%d/-%d", node.linesAdded, node.linesRemoved)
			} else if node.linesAdded > 0 {
				stats = fmt.Sprintf(" +%d", node.linesAdded)
			} else if node.linesRemoved > 0 {
				stats = fmt.Sprintf(" -%d", node.linesRemoved)
			}
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
			if fileIdx > 0 {
				allLines = append(allLines, "")
				allLines = append(allLines, diffHunkStyle.Render("════════════════════════════════════"))
			}

			// Add file path header
			allLines = append(allLines, diffFileHeaderStyle.Render("📄 "+selectedFile.Path))

			if len(selectedFile.Hunks) == 0 {
				// File has no hunks (binary file or no changes)
				allLines = append(allLines, panelInfoStyle.Render("No diff content available (binary file or no changes)"))
			} else {
				for _, hunk := range selectedFile.Hunks {
					// Add hunk header
					allLines = append(allLines, diffHunkStyle.Render("─"))

					for _, diffLine := range hunk.Lines {
						var prefix string
						var prefixStyle lipgloss.Style
						var contentStyle lipgloss.Style

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

						// Render prefix and content separately for better styling
						line := prefixStyle.Render(prefix) + " " + contentStyle.Render(diffLine.Content)
						allLines = append(allLines, line)
					}
				}
			}
		}
	}

	// Apply scrolling
	internalHeight := height - 2 // Account for borders
	if internalHeight < 0 {
		internalHeight = 0
	}

	start := m.diffScroll
	if start < 0 {
		start = 0
	}
	if start > len(allLines) {
		start = len(allLines)
	}
	end := min(start+internalHeight, len(allLines))

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
			visibleHeight := m.visibleContentRows()

			maxScroll := max(0, totalLines-visibleHeight)
			scrollPercent := 100
			if maxScroll > 0 {
				scrollPos := m.diffScroll
				if scrollPos < 0 {
					scrollPos = 0
				}
				if scrollPos > maxScroll {
					scrollPos = maxScroll
				}
				scrollPercent = (scrollPos * 100) / maxScroll
			}
			help = append(help, footerScrollStyle.Render(fmt.Sprintf("Scroll: %d%%", scrollPercent)))
		}
	}

	if m.err != nil {
		help = append(help, errorStyle.Render("Error: "+m.err.Error()))
	}

	return footerBaseStyle.Render(strings.Join(help, " • "))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderHelpModal renders the help modal overlay
func (m Model) renderHelpModal() string {
	return m.renderHelp()
}

// renderCommits renders the list of commits for branch comparison
func (m Model) renderCommits(width, height int) string {
	selectedStyle := fileTreeSelectedLineStyle.Width(max(0, width-2))

	// Calculate internal content height
	internalHeight := height - 2
	if internalHeight < 0 {
		internalHeight = 0
	}

	// If no commits ahead, show changes summary
	if len(m.commits) == 0 {
		var lines []string
		lines = append(lines, panelInfoStyle.Render("No commits ahead of main"))

		// Show staged/unstaged file count if available
		if len(m.diffFiles) > 0 {
			stagedCount := 0
			unstagedCount := 0

			// Count files by checking their status
			for _, f := range m.diffFiles {
				if f.ChangeType == Added {
					unstagedCount++
				} else {
					stagedCount++
				}
			}

			lines = append(lines, "")
			lines = append(lines, panelInfoStyle.Render(fmt.Sprintf("Changes: %d files", len(m.diffFiles))))
		}

		content := strings.Join(lines, "\n")

		return m.renderPanel(content, width, height, m.panel == FileTreePanel)
	}

	// Get visible commits
	start := m.scrollOffset
	end := min(start+internalHeight, len(m.commits))
	if start > len(m.commits) {
		start = len(m.commits)
	}
	if end > len(m.commits) {
		end = len(m.commits)
	}
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
