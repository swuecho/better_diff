package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/sergi/go-diff/diffmatchpatch"
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
	// MaxFileSize is the maximum file size we'll diff (10MB)
	MaxFileSize = 10 * 1024 * 1024
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

	// Detached HEAD state, return the commit hash (shortened)
	hashStr := ref.Hash().String()
	if len(hashStr) > 7 {
		return hashStr[:7], nil
	}
	return hashStr, nil
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

		// Skip files that don't have changes in the mode we're interested in
		var relevantChange bool
		if mode == Staged {
			relevantChange = fileStatus.Staging != git.Unmodified
		} else {
			relevantChange = fileStatus.Worktree != git.Unmodified
		}

		if !relevantChange {
			continue
		}

		var changeType ChangeType
		var statusCode string

		if mode == Staged {
			statusCode = string(fileStatus.Staging)
		} else {
			statusCode = string(fileStatus.Worktree)
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

// GetDiff gets the git diff based on mode using go-git
func GetDiff(mode DiffMode, viewMode DiffViewMode) ([]FileDiff, error) {
	if repository == nil {
		if err := OpenRepository(); err != nil {
			return nil, err
		}
	}

	worktree, err := repository.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Get HEAD commit for comparison
	head, err := repository.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	headCommit, err := repository.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	// Get status to find changed files
	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get git status: %w", err)
	}

	var files []FileDiff

	// Collect and sort paths for stable ordering
	paths := make([]string, 0, len(status))
	for path := range status {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// Get the index for staging area contents
	idx, err := repository.Storer.Index()
	if err != nil {
		return nil, fmt.Errorf("failed to get index: %w", err)
	}

	for _, path := range paths {
		fileStatus := status[path]

		// Check if this file is relevant for the current mode
		var relevantChange bool
		if mode == Staged {
			relevantChange = fileStatus.Staging != git.Unmodified
		} else {
			relevantChange = fileStatus.Worktree != git.Unmodified
		}

		if !relevantChange {
			continue
		}

		fileDiff, err := getFileDiff(worktree, idx, headCommit, path, mode, *fileStatus)
		if err != nil {
			// Log error but continue with other files
			continue
		}

		if fileDiff != nil {
			files = append(files, *fileDiff)
		}
	}

	return files, nil
}

// getFileDiff generates a FileDiff for a single file using go-git
func getFileDiff(worktree *git.Worktree, idx *index.Index, headCommit *object.Commit, path string, mode DiffMode, fileStatus git.FileStatus) (*FileDiff, error) {
	var oldContent, newContent []byte
	var changeType ChangeType

	// Determine change type
	var statusCode string
	if mode == Staged {
		statusCode = string(fileStatus.Staging)
	} else {
		statusCode = string(fileStatus.Worktree)
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

	if mode == Staged {
		// Staged: compare index vs HEAD
		// Get old content from HEAD
		oldFile, err := headCommit.File(path)
		if err == nil {
			oldReader, err := oldFile.Reader()
			if err == nil {
				oldContent, err = readAll(oldReader)
				oldReader.Close()
				if err != nil {
					return nil, fmt.Errorf("failed to read old file %s: %w", path, err)
				}
				if len(oldContent) > MaxFileSize {
					return nil, fmt.Errorf("file %s too large to diff (%d > %d)", path, len(oldContent), MaxFileSize)
				}
			}
		}

		// Get new content from index
		for _, entry := range idx.Entries {
			if entry.Name == path {
				newBlob, err := object.GetBlob(repository.Storer, entry.Hash)
				if err == nil {
					newReader, err := newBlob.Reader()
					if err == nil {
						newContent, err = readAll(newReader)
						newReader.Close()
						if err != nil {
							return nil, fmt.Errorf("failed to read new file %s from index: %w", path, err)
						}
						if len(newContent) > MaxFileSize {
							return nil, fmt.Errorf("file %s too large to diff (%d > %d)", path, len(newContent), MaxFileSize)
						}
					}
				}
				break
			}
		}
	} else {
		// Unstaged: compare worktree vs index
		// Get old content from index
		for _, entry := range idx.Entries {
			if entry.Name == path {
				oldBlob, err := object.GetBlob(repository.Storer, entry.Hash)
				if err == nil {
					oldReader, err := oldBlob.Reader()
					if err == nil {
						oldContent, err = readAll(oldReader)
						oldReader.Close()
						if err != nil {
							return nil, fmt.Errorf("failed to read old file %s from index: %w", path, err)
						}
						if len(oldContent) > MaxFileSize {
							return nil, fmt.Errorf("file %s too large to diff (%d > %d)", path, len(oldContent), MaxFileSize)
						}
					}
				}
				break
			}
		}

		// Get new content from worktree
		worktreeFile, err := worktree.Filesystem.Open(path)
		if err == nil {
			newContent, err = readAll(worktreeFile)
			worktreeFile.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read new file %s from worktree: %w", path, err)
			}
			if len(newContent) > MaxFileSize {
				return nil, fmt.Errorf("file %s too large to diff (%d > %d)", path, len(newContent), MaxFileSize)
			}
		}
		// If file doesn't exist, newContent remains nil (deleted)
	}

	// Generate diff patch using text diff
	fileDiff := &FileDiff{
		Path:         path,
		ChangeType:   changeType,
		Hunks:        []Hunk{},
		LinesAdded:   0,
		LinesRemoved: 0,
	}

	// Generate hunks from diff
	// Normalize: if content is empty, use empty array
	// Otherwise split by newline and remove trailing empty string from split
	oldLines := splitLines(string(oldContent))
	newLines := splitLines(string(newContent))

	hunks, err := computeHunks(oldLines, newLines)
	if err != nil {
		return nil, fmt.Errorf("failed to compute diff for %s: %w", path, err)
	}
	fileDiff.Hunks = hunks

	// Count lines
	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			if line.Type == LineAdded {
				fileDiff.LinesAdded++
			} else if line.Type == LineRemoved {
				fileDiff.LinesRemoved++
			}
		}
	}

	return fileDiff, nil
}

// computeHunks computes diff hunks using Myers diff algorithm
func computeHunks(oldLines, newLines []string) ([]Hunk, error) {
	dmp := diffmatchpatch.New()

	// Join lines with newline to create the full text
	oldText := strings.Join(oldLines, "\n")
	newText := strings.Join(newLines, "\n")

	// Convert to line-based character encoding
	// This encodes each unique line as a single character for efficient diffing
	oldChars, newChars, lineArray := dmp.DiffLinesToChars(oldText, newText)

	// Compute the diff on the character-encoded text
	charDiffs := dmp.DiffMain(oldChars, newChars, false)

	// Convert character diffs to line diffs manually
	// We need to split the character-encoded text by characters and map each to a line
	// Then merge adjacent diffs of the same type
	type lineDiff struct {
		Type  diffmatchpatch.Operation
		Lines []string
	}

	var lineDiffs []lineDiff

	for _, charDiff := range charDiffs {
		// Each character in charDiff.Text represents one line
		runes := []rune(charDiff.Text)
		var lines []string
		for _, r := range runes {
			// Convert rune to int for array indexing
			idx := int(r)
			if idx < len(lineArray) {
				// Get the line and strip trailing newline if present
				line := lineArray[idx]
				line = strings.TrimSuffix(line, "\n")
				lines = append(lines, line)
			}
		}

		// Merge with previous diff if same type
		if len(lineDiffs) > 0 && lineDiffs[len(lineDiffs)-1].Type == charDiff.Type {
			lineDiffs[len(lineDiffs)-1].Lines = append(lineDiffs[len(lineDiffs)-1].Lines, lines...)
		} else {
			lineDiffs = append(lineDiffs, lineDiff{
				Type:  charDiff.Type,
				Lines: lines,
			})
		}
	}

	// Convert line diffs to hunks
	var hunks []Hunk
	var currentHunk *Hunk

	oldLineNum := 1
	newLineNum := 1

	for _, diff := range lineDiffs {
		if len(diff.Lines) == 0 {
			continue
		}

		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			if currentHunk != nil {
				// Add context lines
				for _, line := range diff.Lines {
					currentHunk.Lines = append(currentHunk.Lines, DiffLine{
						Type:    LineContext,
						Content: line,
					})
					oldLineNum++
					newLineNum++
				}

				// Check if we should close the hunk
				trailingContext := 0
				for i := len(currentHunk.Lines) - 1; i >= 0; i-- {
					if currentHunk.Lines[i].Type == LineContext {
						trailingContext++
					} else {
						break
					}
				}

				// Only close if we have actual changes and enough context
				if trailingContext >= DefaultDiffContext {
					hasChanges := false
					for _, l := range currentHunk.Lines {
						if l.Type != LineContext {
							hasChanges = true
							break
						}
					}

					if hasChanges {
						currentHunk.OldCount = oldLineNum - currentHunk.OldStart - trailingContext
						currentHunk.NewCount = newLineNum - currentHunk.NewStart - trailingContext

						// Trim to keep only DefaultDiffContext lines of trailing context
						if trailingContext > DefaultDiffContext {
							trimCount := trailingContext - DefaultDiffContext
							currentHunk.Lines = currentHunk.Lines[:len(currentHunk.Lines)-trimCount]
						}

						hunks = append(hunks, *currentHunk)
						currentHunk = nil
					}
				}
			} else {
				oldLineNum += len(diff.Lines)
				newLineNum += len(diff.Lines)
			}

		case diffmatchpatch.DiffDelete:
			if currentHunk == nil {
				currentHunk = &Hunk{
					Lines:    []DiffLine{},
					OldStart: oldLineNum,
					NewStart: newLineNum,
				}
			}

			for _, line := range diff.Lines {
				currentHunk.Lines = append(currentHunk.Lines, DiffLine{
					Type:    LineRemoved,
					Content: line,
				})
				oldLineNum++
			}

		case diffmatchpatch.DiffInsert:
			if currentHunk == nil {
				currentHunk = &Hunk{
					Lines:    []DiffLine{},
					OldStart: oldLineNum,
					NewStart: newLineNum,
				}
			}

			for _, line := range diff.Lines {
				currentHunk.Lines = append(currentHunk.Lines, DiffLine{
					Type:    LineAdded,
					Content: line,
				})
				newLineNum++
			}
		}
	}

	// Close the last hunk
	if currentHunk != nil {
		currentHunk.OldCount = oldLineNum - currentHunk.OldStart
		currentHunk.NewCount = newLineNum - currentHunk.NewStart

		// Trim trailing context
		trailingContext := 0
		for i := len(currentHunk.Lines) - 1; i >= 0; i-- {
			if currentHunk.Lines[i].Type == LineContext {
				trailingContext++
			} else {
				break
			}
		}

		if trailingContext > DefaultDiffContext {
			trimCount := trailingContext - DefaultDiffContext
			currentHunk.Lines = currentHunk.Lines[:len(currentHunk.Lines)-trimCount]
			currentHunk.OldCount -= trimCount
			currentHunk.NewCount -= trimCount
		}

		hunks = append(hunks, *currentHunk)
	}

	return hunks, nil
}

// readAll reads all content from an io.Reader
func readAll(r io.Reader) ([]byte, error) {
	b := new(bytes.Buffer)
	_, err := b.ReadFrom(r)
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// splitLines splits content by newline and normalizes the result
// It removes the trailing empty string that results from splitting text with a trailing newline
// For example: "a\nb\n" -> ["a", "b"] instead of ["a", "b", ""]
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}

	lines := strings.Split(content, "\n")

	// Remove trailing empty string if present (from trailing newline)
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}
