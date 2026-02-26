package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const appVersion = "1.0.0"

func main() {
	if handled := handleCLIArgs(os.Args[1:]); handled {
		return
	}

	// Initialize git service
	gitService, err := NewGitService()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing git service: %v\n", err)
		os.Exit(1)
	}

	// Get git root path for logger fallback
	gitRootPath, _ := gitService.GetRootPath()

	// Initialize logger (tries /tmp first, then repo parent dir)
	logger, err := NewLogger(INFO, gitRootPath)
	if err != nil {
		// Log the error but continue - logger will fall back to stderr
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}
	defer logger.Close()

	logger.Info("better_diff starting", map[string]interface{}{
		"version": appVersion,
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

func handleCLIArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}

	if isHelpArg(args[0]) {
		fmt.Printf("better_diff %s\n", appVersion)
		return true
	}

	if len(args) >= 2 && strings.EqualFold(args[0], "diff") && isHelpArg(args[1]) {
		fmt.Printf("better_diff %s\n", appVersion)
		return true
	}

	return false
}

func isHelpArg(arg string) bool {
	return strings.EqualFold(arg, "help") || arg == "-h" || arg == "--help"
}
