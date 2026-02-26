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

	// Get git root path for logger fallback.
	gitRootPath := ""
	if rootPath, err := gitService.GetRootPath(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to get git root path: %v\n", err)
	} else {
		gitRootPath = rootPath
	}

	// Initialize logger (tries /tmp first, then repo root).
	logger, err := NewLogger(INFO, gitRootPath)
	if err != nil {
		// Log the error but continue - logger will fall back to stderr
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close logger: %v\n", err)
		}
	}()

	logger.Info("better_diff starting", map[string]any{
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

	if shouldPrintVersion(args) {
		printVersion()
		return true
	}

	return false
}

func shouldPrintVersion(args []string) bool {
	if len(args) == 0 {
		return false
	}

	if isHelpArg(args[0]) {
		return true
	}

	return len(args) >= 2 && strings.EqualFold(args[0], "diff") && isHelpArg(args[1])
}

func printVersion() {
	fmt.Printf("better_diff %s\n", appVersion)
}

func isHelpArg(arg string) bool {
	return strings.EqualFold(arg, "help") || arg == "-h" || arg == "--help"
}
