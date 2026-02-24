package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Initialize git service
	gitService, err := NewGitService()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing git service: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger (can be made configurable)
	logger := NewLogger(INFO)
	logger.Info("better_diff starting", map[string]interface{}{
		"version": "1.0.0",
	})

	// Create model with dependency injection
	model := NewModel(gitService, logger)

	// Create program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // Use alternate screen
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	// Start program
	if _, err := p.Run(); err != nil {
		logger.Error("Program error", err, nil)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Log any errors that occurred
	if logger.HasErrors() {
		stats := logger.GetStats()
		fmt.Fprintf(os.Stderr, "\nCompleted with %d error(s)\n", stats.TotalErrors)
		if stats.TotalWarnings > 0 {
			fmt.Fprintf(os.Stderr, "Warnings: %d\n", stats.TotalWarnings)
		}
	}
}
