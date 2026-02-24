package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// ChangeType represents the type of change
type ChangeType int

const (
	Modified ChangeType = iota
	Added
	Deleted
	Renamed
)

// FileDiff represents a file with its changes
type FileDiff struct {
	Path       string
	OldPath    string // for renames
	ChangeType ChangeType
	Hunks      []Hunk
}

// Hunk represents a section of changes
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []DiffLine
}

// DiffLine represents a single line in a diff
type DiffLine struct {
	Type    LineType
	Content string
	LineNum int // 0 for context lines
}

// LineType represents the type of line in a diff
type LineType int

const (
	LineContext LineType = iota
	LineAdded
	LineRemoved
)

// DiffMode represents whether we're showing staged or unstaged changes
type DiffMode int

const (
	Unstaged DiffMode = iota
	Staged
)

// GetDiff gets the git diff based on mode
func GetDiff(mode DiffMode) ([]FileDiff, error) {
	var cmd *exec.Cmd
	if mode == Staged {
		cmd = exec.Command("git", "diff", "--cached", "--unified=5")
	} else {
		cmd = exec.Command("git", "diff", "--unified=5")
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to get git diff: %w", err)
	}

	return parseDiff(out.String())
}

// GetChangedFiles gets a list of changed files (for tree view)
func GetChangedFiles(mode DiffMode) ([]FileDiff, error) {
	var cmd *exec.Cmd
	if mode == Staged {
		cmd = exec.Command("git", "diff", "--cached", "--name-status")
	} else {
		cmd = exec.Command("git", "diff", "--name-status")
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to get changed files: %w", err)
	}

	return parseChangedFiles(out.String())
}

// parseDiff parses git diff output
func parseDiff(diff string) ([]FileDiff, error) {
	var files []FileDiff
	scanner := bufio.NewScanner(strings.NewReader(diff))

	var currentFile *FileDiff
	var currentHunk *Hunk

	for scanner.Scan() {
		line := scanner.Text()

		// Check for new file
		if strings.HasPrefix(line, "diff --git") {
			if currentFile != nil {
				// Append the last hunk if there is one
				if currentHunk != nil {
					currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
					currentHunk = nil
				}
				files = append(files, *currentFile)
			}
			currentFile = &FileDiff{
				Hunks: []Hunk{},
			}

			// Extract file path from "diff --git a/path b/path"
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// Use parts[3] which is "b/path" and remove the "b/" prefix
				currentFile.Path = strings.TrimPrefix(parts[3], "b/")
			}
		} else if strings.HasPrefix(line, "new file") {
			if currentFile != nil {
				currentFile.ChangeType = Added
			}
		} else if strings.HasPrefix(line, "deleted file") {
			if currentFile != nil {
				currentFile.ChangeType = Deleted
			}
		} else if strings.HasPrefix(line, "rename from") {
			if currentFile != nil {
				currentFile.OldPath = strings.TrimPrefix(line, "rename from ")
				currentFile.ChangeType = Renamed
			}
		} else if strings.HasPrefix(line, "rename to") {
			if currentFile != nil {
				currentFile.Path = strings.TrimPrefix(line, "rename to ")
			}
		} else if strings.HasPrefix(line, "@@") {
			// Parse hunk header: @@ -old_start,old_count +new_start,new_count @@
			if currentFile != nil {
				// If there's a previous hunk, append it first
				if currentHunk != nil {
					currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
				}
				currentHunk = &Hunk{
					Lines: []DiffLine{},
				}
				fmt.Sscanf(line, "@@ -%d,%d +%d,%d @@", &currentHunk.OldStart, &currentHunk.OldCount, &currentHunk.NewStart, &currentHunk.NewCount)
			}
		} else if currentHunk != nil {
			// Parse diff lines
			var lineType LineType
			var content string
			if strings.HasPrefix(line, "+") {
				lineType = LineAdded
				content = strings.TrimPrefix(line, "+")
			} else if strings.HasPrefix(line, "-") {
				lineType = LineRemoved
				content = strings.TrimPrefix(line, "-")
			} else {
				lineType = LineContext
				content = strings.TrimPrefix(line, " ")
			}
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				Type:    lineType,
				Content: content,
			})
		}
	}

	if currentFile != nil {
		// Append the last hunk if there is one
		if currentHunk != nil {
			currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
		}
		files = append(files, *currentFile)
	}

	return files, scanner.Err()
}

// parseChangedFiles parses git diff --name-status output
func parseChangedFiles(output string) ([]FileDiff, error) {
	var files []FileDiff
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		status := parts[0]
		path := parts[1]

		var changeType ChangeType
		switch status {
		case "M":
			changeType = Modified
		case "A":
			changeType = Added
		case "D":
			changeType = Deleted
		case "R":
			changeType = Renamed
		default:
			changeType = Modified
		}

		files = append(files, FileDiff{
			Path:       path,
			ChangeType: changeType,
			Hunks:      []Hunk{},
		})
	}

	return files, scanner.Err()
}

// GetRootPath gets the git repository root path
func GetRootPath() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get git root: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

// GetCurrentBranch gets the current git branch
func GetCurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}
