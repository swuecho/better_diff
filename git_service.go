package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// sortStrings sorts a slice of strings in place
func sortStrings(s []string) {
	sort.Strings(s)
}

// GitService encapsulates all git operations
type GitService struct {
	repo *git.Repository
}

// NewGitService creates a new GitService instance
func NewGitService() (*GitService, error) {
	repoPath, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open git repository at %s: %w", repoPath, err)
	}

	return &GitService{repo: repo}, nil
}

// GetRepository returns the underlying git repository (for advanced usage)
func (gs *GitService) GetRepository() *git.Repository {
	return gs.repo
}

// GetRootPath gets the git repository root path
func (gs *GitService) GetRootPath() (string, error) {
	worktree, err := gs.repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	return worktree.Filesystem.Root(), nil
}

// GetCurrentBranch gets the current git branch
func (gs *GitService) GetCurrentBranch() (string, error) {
	ref, err := gs.repo.Head()
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
func (gs *GitService) GetChangedFiles(mode DiffMode) ([]FileDiff, error) {
	worktree, err := gs.repo.Worktree()
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
	sortStrings(paths)

	var files []FileDiff
	for _, path := range paths {
		fileStatus := status[path]

		// Skip untracked files in staged mode (they're not staged yet)
		if mode == Staged && status.IsUntracked(path) {
			continue
		}

		// For unstaged mode, include untracked files
		// For staged mode, only include files with staged changes
		var relevantChange bool
		if mode == Staged {
			relevantChange = fileStatus.Staging != git.Unmodified
		} else {
			// In unstaged mode, include files with worktree changes OR untracked files
			relevantChange = fileStatus.Worktree != git.Unmodified || status.IsUntracked(path)
		}

		if !relevantChange {
			continue
		}

		var changeType ChangeType
		var statusCode string

		// Check if file is untracked (only relevant in unstaged mode)
		if mode == Unstaged && status.IsUntracked(path) {
			changeType = Added // Untracked files are treated as "to be added"
		} else {
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
func (gs *GitService) GetDiff(mode DiffMode, viewMode DiffViewMode, logger *Logger) ([]FileDiff, error) {
	worktree, err := gs.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Get HEAD commit for comparison
	head, err := gs.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	headCommit, err := gs.repo.CommitObject(head.Hash())
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
	sortStrings(paths)

	// Get the index for staging area contents
	idx, err := gs.repo.Storer.Index()
	if err != nil {
		return nil, fmt.Errorf("failed to get index: %w", err)
	}

	for _, path := range paths {
		fileStatus := status[path]

		// Skip untracked files in staged mode (they're not staged yet)
		if mode == Staged && status.IsUntracked(path) {
			continue
		}

		// For unstaged mode, include untracked files
		// For staged mode, only include files with staged changes
		var relevantChange bool
		if mode == Staged {
			relevantChange = fileStatus.Staging != git.Unmodified
		} else {
			// In unstaged mode, include files with worktree changes OR untracked files
			relevantChange = fileStatus.Worktree != git.Unmodified || status.IsUntracked(path)
		}

		if !relevantChange {
			continue
		}

		fileDiff, err := gs.getFileDiff(worktree, idx, headCommit, path, mode, *fileStatus, logger)
		if err != nil {
			// Log error but continue with other files
			if logger != nil {
				logger.Error("Failed to get file diff", err, map[string]interface{}{
					"file": path,
					"mode": mode,
				})
			}
			continue
		}

		if fileDiff != nil {
			files = append(files, *fileDiff)
		}
	}

	return files, nil
}

// getFileDiff generates a FileDiff for a single file
func (gs *GitService) getFileDiff(worktree *git.Worktree, idx *index.Index, headCommit *object.Commit, path string, mode DiffMode, fileStatus git.FileStatus, logger *Logger) (*FileDiff, error) {
	var oldContent, newContent []byte
	var changeType ChangeType

	// Check if this is an untracked file (only in unstaged mode)
	isUntracked := mode == Unstaged && (fileStatus.Worktree == git.Untracked)

	if isUntracked {
		// For untracked files, show full file content as "new"
		changeType = Added

		// Read file from working tree
		file, err := worktree.Filesystem.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open untracked file %s: %w", path, err)
		}

		newContent, err = readAll(file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read untracked file %s: %w", path, err)
		}

		if len(newContent) > MaxFileSize {
			if logger != nil {
				logger.Warn("Untracked file too large to display", map[string]interface{}{
					"file": path,
					"size": len(newContent),
					"max":  MaxFileSize,
				})
			}
			return nil, fmt.Errorf("untracked file %s too large to display (%d > %d)", path, len(newContent), MaxFileSize)
		}

		// oldContent remains empty (file doesn't exist in git history)
	} else {
		// Determine change type for tracked files
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
						if logger != nil {
							logger.Warn("File too large to diff", map[string]interface{}{
								"file": path,
								"size": len(oldContent),
								"max":  MaxFileSize,
							})
						}
						return nil, fmt.Errorf("file %s too large to diff (%d > %d)", path, len(oldContent), MaxFileSize)
					}
				}
			}

			// Get new content from index
			for _, entry := range idx.Entries {
				if entry.Name == path {
					newBlob, err := object.GetBlob(gs.repo.Storer, entry.Hash)
					if err == nil {
						newReader, err := newBlob.Reader()
						if err == nil {
							newContent, err = readAll(newReader)
							newReader.Close()
							if err != nil {
								return nil, fmt.Errorf("failed to read new file %s from index: %w", path, err)
							}
							if len(newContent) > MaxFileSize {
								if logger != nil {
									logger.Warn("File too large to diff", map[string]interface{}{
										"file": path,
										"size": len(newContent),
										"max":  MaxFileSize,
									})
								}
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
					oldBlob, err := object.GetBlob(gs.repo.Storer, entry.Hash)
					if err == nil {
						oldReader, err := oldBlob.Reader()
						if err == nil {
							oldContent, err = readAll(oldReader)
							oldReader.Close()
							if err != nil {
								return nil, fmt.Errorf("failed to read old file %s from index: %w", path, err)
							}
							if len(oldContent) > MaxFileSize {
								if logger != nil {
									logger.Warn("File too large to diff", map[string]interface{}{
										"file": path,
										"size": len(oldContent),
										"max":  MaxFileSize,
									})
								}
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
					if logger != nil {
						logger.Warn("File too large to diff", map[string]interface{}{
							"file": path,
							"size": len(newContent),
							"max":  MaxFileSize,
						})
					}
					return nil, fmt.Errorf("file %s too large to diff (%d > %d)", path, len(newContent), MaxFileSize)
				}
			}
		}
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
