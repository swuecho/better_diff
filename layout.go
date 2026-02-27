package main

// Layout constants for the TUI
const (
	// Header and footer dimensions
	headerRows       = 2 // Number of rows for header (title + separator)
	headerSearchRows = 1 // Extra rows when search bar is visible
	footerRows       = 1 // Number of rows for footer

	// Panel layout
	panelBorderRows   = 2 // Rows consumed by panel borders (top + bottom)
	fileTreeWidthRatio = 3 // File tree gets 1/fileTreeWidthRatio of total width

	// Line number formatting
	lineNumWidth = 4 // Width in characters for each line number column

	// Help modal dimensions
	helpModalMaxWidth  = 60 // Maximum width of help modal
	helpModalMaxHeight = 30 // Maximum height of help modal
	helpModalPadding   = 4  // Padding around help modal (2 on each side)

	// Branch compare limits
	maxCommitsAhead = 50 // Maximum number of commits to show ahead of main
)

// contentHeight calculates the available content height given total height and search mode
func contentHeight(totalHeight int, searchMode bool) int {
	header := headerRows
	if searchMode {
		header += headerSearchRows
	}
	return max(1, totalHeight-header-footerRows)
}

// panelContentHeight calculates the content height inside a panel (accounting for borders)
func panelContentHeight(panelHeight int) int {
	return max(0, panelHeight-panelBorderRows)
}

// fileTreeWidth calculates the width for the file tree panel
func fileTreeWidth(totalWidth int) int {
	return totalWidth / fileTreeWidthRatio
}

// diffPanelWidth calculates the width for the diff panel
func diffPanelWidth(totalWidth int) int {
	return totalWidth - fileTreeWidth(totalWidth)
}

// helpModalDimensions calculates the dimensions for the help modal
func helpModalDimensions(screenWidth, screenHeight int) (width, height int) {
	width = min(helpModalMaxWidth, screenWidth-helpModalPadding)
	height = min(helpModalMaxHeight, screenHeight-helpModalPadding)
	return width, height
}
