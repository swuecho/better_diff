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

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	gitService, err := NewGitService()
	if err != nil {
		return fmt.Errorf("initialize git service: %w", err)
	}

	logger := initLogger(gitService)
	defer func() {
		if closeErr := logger.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: close logger: %v\n", closeErr)
		}
	}()

	logger.Info("better_diff starting", map[string]any{
		"version": appVersion,
	})

	program := tea.NewProgram(
		NewModel(gitService, logger),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := program.Run(); err != nil {
		logger.Error("program error", err, nil)
		return fmt.Errorf("run program: %w", err)
	}

	reportLoggerStats(logger)
	return nil
}

func initLogger(gitService *GitService) *Logger {
	gitRootPath, err := gitService.GetRootPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: get git root path: %v\n", err)
		gitRootPath = ""
	}

	logger, err := NewLogger(INFO, gitRootPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	return logger
}

func reportLoggerStats(logger *Logger) {
	if !logger.HasErrors() {
		return
	}

	stats := logger.GetStats()
	fmt.Fprintf(os.Stderr, "\ncompleted with %d error(s)\n", stats.TotalErrors)
	if stats.TotalWarnings > 0 {
		fmt.Fprintf(os.Stderr, "warnings: %d\n", stats.TotalWarnings)
	}
}

func handleCLIArgs(args []string) bool {
	if !shouldPrintVersion(args) {
		return false
	}
	printVersion()
	return true
}

func shouldPrintVersion(args []string) bool {
	if len(args) == 0 {
		return false
	}
	return isHelpArg(args[0]) || (len(args) >= 2 && strings.EqualFold(args[0], "diff") && isHelpArg(args[1]))
}

func printVersion() {
	fmt.Printf("better_diff %s\n", appVersion)
}

func isHelpArg(arg string) bool {
	return strings.EqualFold(arg, "help") || arg == "-h" || arg == "--help"
}
