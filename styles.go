package main

import (
	"github.com/charmbracelet/lipgloss"
)

// Color constants for consistent theming
var (
	// Primary colors
	colorBlue   = lipgloss.Color("blue")
	colorYellow = lipgloss.Color("yellow")
	colorWhite  = lipgloss.Color("white")

	// Gray scale (for subtle elements)
	colorGray243 = lipgloss.Color("243") // Medium gray
	colorGray244 = lipgloss.Color("244") // Subtle gray
	colorGray245 = lipgloss.Color("245") // Light gray
	colorGray235 = lipgloss.Color("235") // Dark gray (background)
	colorGray237 = lipgloss.Color("237") // Border gray

	// Diff colors
	colorGreen142 = lipgloss.Color("142") // Soft green (diff content)
	colorGreen86  = lipgloss.Color("86")  // Bright green (added lines)
	colorRed203   = lipgloss.Color("203") // Soft red (diff content)
	colorRed196   = lipgloss.Color("196") // Bright red (removed lines)

	// Accent colors
	colorSoftBlue75 = lipgloss.Color("75")  // Soft blue (selection)
	colorSoftYellow = lipgloss.Color("229") // Soft warm yellow
)

// Predefined styles for reuse
var (
	// Header styles
	headerStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	modeIndicatorStyle = lipgloss.NewStyle().
				Foreground(colorYellow).
				Bold(true)

	viewModeIndicatorStyle = lipgloss.NewStyle().
				Foreground(colorGreen86).
				Bold(true)

	headerSeparatorStyle = lipgloss.NewStyle().
				Foreground(colorGray237)

	subtleStyle = lipgloss.NewStyle().
			Foreground(colorGray244)

	// File tree styles
	fileStyle = lipgloss.NewStyle().
			Foreground(colorGray243).
			Bold(true)

	dirStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true)

	addedStyle = lipgloss.NewStyle().
			Foreground(colorGreen142). // Soft green matching diff panel
			Bold(true)

	modifiedStyle = lipgloss.NewStyle().
			Foreground(colorSoftYellow). // Soft warm yellow for modified files
			Bold(true)

	deletedStyle = lipgloss.NewStyle().
			Foreground(colorRed203). // Soft red for deleted files
			Bold(true)

	// Selection styles
	selectedStyle = lipgloss.NewStyle().
			Foreground(colorSoftBlue75).
			Bold(true).
			Background(colorGray235)

	fileTreeSelectedLineStyle = lipgloss.NewStyle().
					Background(colorGray235)

	// Diff styles
	diffAddedStyle = lipgloss.NewStyle().
			Foreground(colorGreen142).
			Bold(true)

	diffRemovedStyle = lipgloss.NewStyle().
				Foreground(colorRed203).
				Bold(true)

	diffAddedPrefixStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("46")). // Vibrant bright green for + prefix
				Bold(true)

	diffRemovedPrefixStyle = lipgloss.NewStyle().
				Foreground(colorRed196).
				Bold(true)

	diffContextStyle = lipgloss.NewStyle().
				Foreground(colorGray245)

	diffLineNumStyle = lipgloss.NewStyle().
				Foreground(colorGray244)

	diffSubtleStyle = lipgloss.NewStyle().
			Foreground(colorGray244)

	diffHunkStyle = lipgloss.NewStyle().
			Foreground(colorGray244)

	diffFileHeaderStyle = lipgloss.NewStyle().
				Foreground(colorSoftBlue75).
				Bold(true)

	diffCommitHeaderStyle = lipgloss.NewStyle().
				Foreground(colorGreen86).
				Bold(true)

	// Stats styles
	statsStyle = lipgloss.NewStyle().
			Foreground(colorSoftYellow).
			Bold(true)

	statsSubtleStyle = lipgloss.NewStyle().
				Foreground(colorGray244)

	// Border styles
	panelBaseStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGray237)

	panelActiveStyle = panelBaseStyle.BorderForeground(colorBlue)

	// Help modal styles
	helpTitleStyle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Bold(true).
			Underline(true)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorSoftYellow).
			Bold(true).
			Width(8)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorGray243)

	helpSectionStyle = lipgloss.NewStyle().
				Foreground(colorSoftBlue75).
				Bold(true).
				MarginTop(1)

	// Error styles
	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed203).
			Bold(true)

	panelInfoStyle = lipgloss.NewStyle().
			Foreground(colorGray243).
			Italic(true)

	footerBaseStyle = lipgloss.NewStyle().
			Foreground(colorGray243)

	footerKeyStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	footerScrollStyle = lipgloss.NewStyle().
				Foreground(colorYellow)

	commitHashStyle = lipgloss.NewStyle().
			Foreground(colorGreen86).
			Bold(true)

	commitAuthorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("147"))

	commitDateStyle = lipgloss.NewStyle().
			Foreground(colorGray245)

	commitMessageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("223"))

	// Status indicator styles
	statusAddedStyle = lipgloss.NewStyle().
				Foreground(colorGreen86).
				Bold(true).
				Padding(0, 1)

	statusModifiedStyle = lipgloss.NewStyle().
				Foreground(colorYellow).
				Bold(true).
				Padding(0, 1)

	statusDeletedStyle = lipgloss.NewStyle().
				Foreground(colorRed196).
				Bold(true).
				Padding(0, 1)

	// Search styles
	searchIndicatorStyle = lipgloss.NewStyle().
				Foreground(colorSoftBlue75).
				Bold(true)

	searchPromptStyle = lipgloss.NewStyle().
				Foreground(colorYellow).
				Bold(true)

	searchQueryStyle = lipgloss.NewStyle().
				Foreground(colorWhite).
				Bold(true)

	searchCursorStyle = lipgloss.NewStyle().
				Foreground(colorGreen86).
				Bold(true)

	searchLineStyle = lipgloss.NewStyle().
				Background(colorGray235).
				Padding(0, 1)
)

// GetStatusStyle returns the appropriate style for a change type
func GetStatusStyle(changeType ChangeType) lipgloss.Style {
	switch changeType {
	case Added:
		return statusAddedStyle
	case Modified:
		return statusModifiedStyle
	case Deleted:
		return statusDeletedStyle
	case Renamed:
		return statusModifiedStyle // Same as modified for now
	default:
		return subtleStyle
	}
}

// GetStatusSymbol returns the symbol for a change type
func GetStatusSymbol(changeType ChangeType) string {
	switch changeType {
	case Modified:
		return "M"
	case Added:
		return "A"
	case Deleted:
		return "D"
	case Renamed:
		return "R"
	default:
		return "?"
	}
}
