package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Initialize git repository
	if err := OpenRepository(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Create model
	model := NewModel()

	// Create program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // Use alternate screen
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	// Start program
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
