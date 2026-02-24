package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Check if we're in a git repository
	if !isGitRepo() {
		fmt.Println("Error: Not a git repository")
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

func isGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	return cmd.Run() == nil
}
