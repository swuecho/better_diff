package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
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
	Path         string
	OldPath      string // for renames
	ChangeType   ChangeType
	Hunks        []Hunk
	LinesAdded   int
	LinesRemoved int
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

// DiffViewMode represents how much context to show in diff
type DiffViewMode int

const (
	DiffOnly DiffViewMode = iota
	WholeFile
)

const (
	// DefaultDiffContext is the default number of context lines in diff
	DefaultDiffContext = 5
	// WholeFileContext is a large number to show whole file in diff
	WholeFileContext = 999999
)

// Repository holds the git repository instance
var repository *git.Repository

// OpenRepository opens the git repository at the current directory
func OpenRepository() error {
	repoPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return fmt.Errorf("failed to open git repository at %s: %w", repoPath, err)
	}

	repository = repo
	return nil
}

// GetRootPath gets the git repository root path
func GetRootPath() (string, error) {
	if repository == nil {
		if err := OpenRepository(); err != nil {
			return "", err
		}
	}

	worktree, err := repository.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	return worktree.Filesystem.Root(), nil
}

// GetCurrentBranch gets the current git branch
func GetCurrentBranch() (string, error) {
	if repository == nil {
		if err := OpenRepository(); err != nil {
			return "", err
		}
	}

	ref, err := repository.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD reference: %w", err)
	}

	if ref.Name().IsBranch() {
		return ref.Name().Short(), nil
	}

	// Detached HEAD state, return the commit hash
	return ref.Hash().String()[:7], nil
}

// GetChangedFiles gets a list of changed files (for tree view)
func GetChangedFiles(mode DiffMode) ([]FileDiff, error) {
	if repository == nil {
		if err := OpenRepository(); err != nil {
			return nil, err
		}
	}

	worktree, err := repository.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get git status: %w", err)
	}

	// Collect paths and sort them for stable ordering
	paths := make([]string, 0, len(status))
	for path := range status {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var files []FileDiff
	for _, path := range paths {
		fileStatus := status[path]
		var changeType ChangeType
		var statusCode string

		if mode == Staged {
			statusCode = string(fileStatus.Staging)
			// Skip unstaged-only changes
			if fileStatus.Staging == git.Unmodified {
				continue
			}
		} else {
			statusCode = string(fileStatus.Worktree)
			// Skip staged-only changes
			if fileStatus.Worktree == git.Unmodified {
				continue
			}
		}

		switch statusCode {
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
			Path:         path,
			ChangeType:   changeType,
			Hunks:        []Hunk{},
			LinesAdded:   0,
			LinesRemoved: 0,
		})
	}

	return files, nil
}

// GetDiff gets the git diff based on mode
// Uses git CLI for diff generation as it provides better compatibility
// with various edge cases than go-git's diff implementation
func GetDiff(mode DiffMode, viewMode DiffViewMode) ([]FileDiff, error) {
	if repository == nil {
		if err := OpenRepository(); err != nil {
			return nil, err
		}
	}

	return getDiffAlternative(mode, viewMode)
}

// getDiffAlternative uses git diff command output parsing
func getDiffAlternative(mode DiffMode, viewMode DiffViewMode) ([]FileDiff, error) {
	unified := fmt.Sprintf("%d", DefaultDiffContext)
	if viewMode == WholeFile {
		unified = fmt.Sprintf("%d", WholeFileContext)
	}

	var args []string
	if mode == Staged {
		args = []string{"diff", "--cached", "--unified=" + unified}
	} else {
		args = []string{"diff", "--unified=" + unified}
	}

	output, err := runGitCommand(args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get git diff (mode=%v): %w", mode, err)
	}

	return parseDiff(output)
}

// runGitCommand runs a git command and returns the output
func runGitCommand(args ...string) (string, error) {
	worktree, err := repository.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	// Get the repository path
	repoPath := worktree.Filesystem.Root()

	// Build command with proper quoting
	cmdArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", cmdArgs...)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git command %v failed: %w, stderr: %s", args, err, stderr.String())
	}

	return out.String(), nil
}

// parseDiff parses git diff output
func parseDiff(diffStr string) ([]FileDiff, error) {
	var files []FileDiff
	scanner := bufio.NewScanner(strings.NewReader(diffStr))

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
				Hunks:        []Hunk{},
				LinesAdded:   0,
				LinesRemoved: 0,
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
				currentFile.LinesAdded++
			} else if strings.HasPrefix(line, "-") {
				lineType = LineRemoved
				content = strings.TrimPrefix(line, "-")
				currentFile.LinesRemoved++
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
