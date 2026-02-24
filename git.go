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
				oldContent, _ = readAll(oldReader)
				oldReader.Close()
			}
		}

		// Get new content from index
		for _, entry := range idx.Entries {
			if entry.Name == path {
				newBlob, err := object.GetBlob(repository.Storer, entry.Hash)
				if err == nil {
					newReader, err := newBlob.Reader()
					if err == nil {
						newContent, _ = readAll(newReader)
						newReader.Close()
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
						oldContent, _ = readAll(oldReader)
						oldReader.Close()
					}
				}
				break
			}
		}

		// Get new content from worktree
		worktreeFile, err := worktree.Filesystem.Open(path)
		if err == nil {
			newContent, _ = readAll(worktreeFile)
			worktreeFile.Close()
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
	oldLines := strings.Split(string(oldContent), "\n")
	newLines := strings.Split(string(newContent), "\n")

	hunks := computeHunks(oldLines, newLines)
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

// computeHunks computes diff hunks using a simple line-by-line comparison
func computeHunks(oldLines, newLines []string) []Hunk {
	var hunks []Hunk
	var currentHunk *Hunk

	oldLen := len(oldLines)
	newLen := len(newLines)
	maxLen := oldLen
	if newLen > maxLen {
		maxLen = newLen
	}

	// Simple line-by-line diff (not optimal but works for basic cases)
	for i := 0; i < maxLen; i++ {
		oldLine := ""
		newLine := ""

		if i < oldLen {
			oldLine = oldLines[i]
		}
		if i < newLen {
			newLine = newLines[i]
		}

		if oldLine == newLine {
			// Context line
			if currentHunk != nil && len(currentHunk.Lines) > 0 {
				// Add context to existing hunk
				currentHunk.Lines = append(currentHunk.Lines, DiffLine{
					Type:    LineContext,
					Content: oldLine,
				})
			}
		} else {
			// Difference detected
			if currentHunk == nil {
				currentHunk = &Hunk{
					Lines: []DiffLine{},
				}
			}

			// Add old line if exists (deletion)
			if i < oldLen {
				currentHunk.Lines = append(currentHunk.Lines, DiffLine{
					Type:    LineRemoved,
					Content: oldLine,
				})
			}

			// Add new line if exists (addition)
			if i < newLen {
				currentHunk.Lines = append(currentHunk.Lines, DiffLine{
					Type:    LineAdded,
					Content: newLine,
				})
			}
		}
	}

	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}

	return hunks
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
