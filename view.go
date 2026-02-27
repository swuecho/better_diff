package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	headerRows      = 2
	headerSearchRows = 1 // Extra rows when search bar is visible
	footerRows      = 1
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
	headerHeight := headerRows
	if m.searchMode {
		headerHeight += headerSearchRows
	}
	contentHeight := m.height - headerHeight - footerRows
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

	// Show search indicator or help hint
	if m.searchQuery != "" {
		filtered := len(m.flattenTree())
		parts = append(parts, searchIndicatorStyle.Render(fmt.Sprintf("Filter: %q (%d)", m.searchQuery, filtered)))
	} else if !m.showHelp {
		parts = append(parts, subtleStyle.Render("Press ? for help"))
	}

	header := strings.Join(parts, " ")

	// Show search input bar if in search mode
	if m.searchMode {
		searchBar := m.renderSearchBar()
		separator := headerSeparatorStyle.Render(strings.Repeat("─", m.width))
		return lipgloss.JoinVertical(lipgloss.Left, header, separator, searchBar)
	}

	separator := headerSeparatorStyle.Render(strings.Repeat("─", m.width))

	return lipgloss.JoinVertical(lipgloss.Left, header, separator)
}

// renderSearchBar renders the search input bar
func (m Model) renderSearchBar() string {
	prompt := searchPromptStyle.Render("Search: ")
	query := searchQueryStyle.Render(m.searchQuery)
	cursor := searchCursorStyle.Render("█")
	hint := subtleStyle.Render("  [Enter] confirm  [Esc] cancel  [Backspace] delete")

	searchLine := prompt + query + cursor + hint
	return searchLineStyle.Width(m.width).Render(searchLine)
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
		lines = append(lines, renderTreeNodeLine(node, isSelected, m.panel == FileTreePanel, selectedStyle))
	}

	// Build content as a single string
	content := strings.Join(lines, "\n")

	// Apply panel styling with border
	return m.renderPanel(content, width, height, m.panel == FileTreePanel)
}

func renderTreeNodeLine(node TreeNode, isSelected, isTreePanelActive bool, selectedStyle lipgloss.Style) string {
	indicator, lineStyle := treeNodeIndicatorAndStyle(node)
	line := treeNodePrefix(node) + indicator + " " + node.name

	if !node.isDir && (node.linesAdded > 0 || node.linesRemoved > 0) {
		line += statsStyle.Render(formatLineStats(node.linesAdded, node.linesRemoved))
	}

	if isSelected && isTreePanelActive {
		return selectedStyle.Render(line)
	}
	return lineStyle.Render(line)
}

func treeNodePrefix(node TreeNode) string {
	prefix := strings.Repeat("  ", node.depth)
	if !node.isDir {
		return prefix + "  "
	}
	if node.isExpanded {
		return prefix + "▼ "
	}
	return prefix + "▶ "
}

func treeNodeIndicatorAndStyle(node TreeNode) (string, lipgloss.Style) {
	if node.isDir {
		return treeNodeIndicator(node.changeType), dirStyle
	}

	switch node.changeType {
	case Added:
		return "+", addedStyle
	case Deleted:
		return "-", deletedStyle
	default:
		return "●", modifiedStyle
	}
}

func treeNodeIndicator(changeType ChangeType) string {
	switch changeType {
	case Added:
		return "+"
	case Deleted:
		return "-"
	default:
		return "●"
	}
}

func (m Model) renderDiffPanel(width, height int) string {
	allLines := m.buildDiffPanelLines()
	content := strings.Join(visiblePaddedLines(allLines, m.diffScroll, panelContentHeight(height)), "\n")

	// Apply panel styling with border
	return m.renderPanel(content, width, height, m.panel == DiffPanel)
}

func (m Model) buildDiffPanelLines() []string {
	filesToRender := m.getSelectedDiffFiles()
	lines := make([]string, 0)

	if m.diffMode == BranchCompare {
		lines = append(lines, diffCommitHeaderStyle.Render("Branch Compare: current working tree vs default branch"), "")
	}

	if len(filesToRender) == 0 {
		return append(lines, panelInfoStyle.Render(m.diffPanelEmptyMessage()))
	}

	for fileIdx, selectedFile := range filesToRender {
		lines = appendRenderedFileDiffLines(lines, selectedFile, fileIdx > 0)
	}
	return lines
}

func (m Model) diffPanelEmptyMessage() string {
	if m.diffMode == BranchCompare {
		return "Select a file to view unified changes"
	}
	return "Select a file to view diff"
}

func visiblePaddedLines(allLines []string, scrollOffset, height int) []string {
	start, end := visibleRange(scrollOffset, height, len(allLines))

	var lines []string
	if start < len(allLines) && end > start {
		lines = allLines[start:end]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
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

	// Render line numbers
	lineNums := renderDiffLineNumbers(diffLine)

	return lineNums + prefixStyle.Render(prefix) + " " + contentStyle.Render(diffLine.Content)
}

// renderDiffLineNumbers renders the old and new line numbers for a diff line
func renderDiffLineNumbers(diffLine DiffLine) string {
	oldNum := formatLineNumber(diffLine.OldLineNum)
	newNum := formatLineNumber(diffLine.NewLineNum)

	return diffLineNumStyle.Render(oldNum+" "+newNum) + " "
}

// formatLineNumber formats a line number for display
func formatLineNumber(num int) string {
	if num == 0 {
		return strings.Repeat(" ", 4) // 4 spaces for alignment when no line number
	}
	return fmt.Sprintf("%4d", num)
}

func (m Model) renderFooter() string {
	help := m.footerHelpItems()
	help = m.appendFooterScroll(help)
	help = m.appendFooterError(help)
	return footerBaseStyle.Render(strings.Join(help, " • "))
}

func (m Model) footerHelpItems() []string {
	help := m.contextualFooterHelp()
	if m.diffViewMode == DiffOnly {
		help = append(help,
			footerKeyStyle.Render("[o/O]")+" Expand/Reset",
			footerKeyStyle.Render("[Tab]")+" Switch Panel",
		)
	}
	return append(help,
		footerKeyStyle.Render("[s]")+" Mode",
		footerKeyStyle.Render("[f]")+" Diff/Whole File",
		footerKeyStyle.Render("[?]")+" Help",
		footerKeyStyle.Render("[q]")+" Quit",
	)
}

func (m Model) contextualFooterHelp() []string {
	if m.searchMode {
		return []string{
			footerKeyStyle.Render("[type]") + " Filter files",
			footerKeyStyle.Render("[Enter]") + " Confirm",
			footerKeyStyle.Render("[Esc]") + " Cancel",
		}
	}

	if m.panel == FileTreePanel {
		return []string{
			footerKeyStyle.Render("[↑↓/j/k]") + " Navigate",
			footerKeyStyle.Render("[PgUp/PgDn]") + " Page",
			footerKeyStyle.Render("[Enter]") + " Select/Expand",
			footerKeyStyle.Render("[/]") + " Search",
		}
	}

	diffNavigationLabel := "Scroll"
	if m.diffViewMode == DiffOnly {
		diffNavigationLabel = "Hunk Jump"
	}
	return []string{
		footerKeyStyle.Render("[↑↓]") + " Scroll",
		footerKeyStyle.Render("[PgUp/PgDn]") + " Page",
		footerKeyStyle.Render("[j/k]") + " " + diffNavigationLabel,
		footerKeyStyle.Render("[gg/G]") + " Top/Bottom",
	}
}

func (m Model) appendFooterScroll(help []string) []string {
	if m.panel != DiffPanel {
		return help
	}
	totalLines := m.getDiffLineCount()
	if totalLines == 0 {
		return help
	}

	scrollPercent := computeScrollPercent(m.diffScroll, totalLines, m.visibleContentRows())
	return append(help, footerScrollStyle.Render(fmt.Sprintf("Scroll: %d%%", scrollPercent)))
}

func (m Model) appendFooterError(help []string) []string {
	if m.err == nil {
		return help
	}
	return append(help, errorStyle.Render("Error: "+m.err.Error()))
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
		return m.renderPanel(m.renderEmptyCommitList(), width, height, m.panel == FileTreePanel)
	}

	// Get visible commits.
	start, end := visibleRange(m.scrollOffset, internalHeight, len(m.commits))
	visibleCommits := m.commits[start:end]

	// Render each commit
	var lines []string
	for i, commit := range visibleCommits {
		globalIndex := start + i
		isSelected := globalIndex == m.selectedIndex
		lines = append(lines, renderCommitLines(commit, isSelected, m.panel == FileTreePanel, selectedStyle)...)
	}

	// Build content as a single string
	content := strings.Join(lines, "\n")

	// Apply panel styling with border
	return m.renderPanel(content, width, height, m.panel == FileTreePanel)
}

func (m Model) renderEmptyCommitList() string {
	lines := []string{panelInfoStyle.Render("No commits ahead of main")}
	if len(m.diffFiles) > 0 {
		lines = append(lines, "", panelInfoStyle.Render(fmt.Sprintf("Changes: %d files", len(m.diffFiles))))
	}
	return strings.Join(lines, "\n")
}

func renderCommitLines(commit Commit, isSelected, isTreePanelActive bool, selectedStyle lipgloss.Style) []string {
	if isSelected && isTreePanelActive {
		header := selectedStyle.Render(commit.ShortHash + " " + commit.Author + " " + commit.Date)
		return []string{header, "    " + commitMessageStyle.Render(commit.Message)}
	}

	line := commitHashStyle.Render(commit.ShortHash) + " " +
		commitAuthorStyle.Render(commit.Author) + " " +
		commitDateStyle.Render(commit.Date) + "\n" +
		"    " + commitMessageStyle.Render(commit.Message)
	return []string{line}
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
