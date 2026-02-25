package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
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

// GetDefaultBranch returns the default branch name (main or master)
func (gs *GitService) GetDefaultBranch() (string, error) {
	// Check for common default branch names
	defaultBranches := []string{"refs/heads/main", "refs/heads/master", "refs/heads/develop"}

	for _, refName := range defaultBranches {
		ref, err := gs.repo.Reference(plumbing.ReferenceName(refName), false)
		if err == nil && ref != nil {
			return ref.Name().Short(), nil
		}
	}

	// Fallback to checking origin
	originMain, err := gs.repo.Reference(plumbing.ReferenceName("refs/remotes/origin/main"), false)
	if err == nil && originMain != nil {
		return "main", nil
	}

	originMaster, err := gs.repo.Reference(plumbing.ReferenceName("refs/remotes/origin/master"), false)
	if err == nil && originMaster != nil {
		return "master", nil
	}

	// Default to main if nothing else found
	return "main", nil
}

// GetCommitsAheadOfMain gets commits that are on the current branch but not on main
func (gs *GitService) GetCommitsAheadOfMain() ([]Commit, error) {
	currentBranch, err := gs.GetCurrentBranch()
	if err != nil {
		return nil, fmt.Errorf("failed to get current branch: %w", err)
	}

	defaultBranch, err := gs.GetDefaultBranch()
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch: %w", err)
	}

	// If we're on the default branch, no commits ahead
	if currentBranch == defaultBranch {
		return []Commit{}, nil
	}

	// Get current branch reference
	currentRef, err := gs.repo.Reference(plumbing.ReferenceName("refs/heads/"+currentBranch), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get current branch reference: %w", err)
	}

	// Get default branch reference
	defaultRef, err := gs.repo.Reference(plumbing.ReferenceName("refs/heads/"+defaultBranch), true)
	if err != nil {
		// Default branch might not exist locally, try to get from HEAD
		defaultRef, err = gs.repo.Reference(plumbing.ReferenceName("refs/remotes/origin/"+defaultBranch), true)
		if err != nil {
			return nil, fmt.Errorf("failed to get default branch reference: %w", err)
		}
	}

	// Get commit objects
	currentCommit, err := gs.repo.CommitObject(currentRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get current commit: %w", err)
	}

	defaultCommit, err := gs.repo.CommitObject(defaultRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get default commit: %w", err)
	}

	// Get merge base
	mergeBase, err := getMergeBase(gs.repo, currentCommit, defaultCommit)
	if err != nil {
		return nil, fmt.Errorf("failed to get merge base: %w", err)
	}

	// Get commits from merge base to current head (exclusive of merge base)
	commitIter, err := gs.repo.Log(&git.LogOptions{
		From:  currentCommit.Hash,
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get commit log: %w", err)
	}

	var commits []Commit
	foundMergeBase := false

	err = commitIter.ForEach(func(c *object.Commit) error {
		if c.Hash.String() == mergeBase.Hash.String() {
			// Stop when we reach the merge base
			foundMergeBase = true
			return fmt.Errorf("stop")
		}

		commits = append(commits, Commit{
			Hash:      c.Hash.String(),
			ShortHash: c.Hash.String()[:7],
			Author:    c.Author.Name,
			Message:   getFirstLine(c.Message),
			Date:      c.Author.When.Format("2006-01-02 15:04"),
		})

		return nil
	})

	if err != nil && err.Error() != "stop" {
		return nil, err
	}

	if !foundMergeBase {
		// If we didn't find merge base, the branches might have diverged
		// Return all commits up to a reasonable limit
		if len(commits) > 50 {
			commits = commits[:50]
		}
	}

	return commits, nil
}

// getMergeBase finds the merge base of two commits
func getMergeBase(repo *git.Repository, commit1, commit2 *object.Commit) (*object.Commit, error) {
	// Simple implementation: find common ancestor
	// Get all ancestors of commit1
	ancestors1 := make(map[string]struct{})
	iter1, err := repo.Log(&git.LogOptions{
		From:  commit1.Hash,
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, err
	}

	iter1.ForEach(func(c *object.Commit) error {
		ancestors1[c.Hash.String()] = struct{}{}
		return nil
	})

	// Find first ancestor of commit2 that's also in ancestors1
	iter2, err := repo.Log(&git.LogOptions{
		From:  commit2.Hash,
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, err
	}

	var mergeBase *object.Commit
	iter2.ForEach(func(c *object.Commit) error {
		if _, ok := ancestors1[c.Hash.String()]; ok {
			mergeBase = c
			return fmt.Errorf("found")
		}
		return nil
	})

	if mergeBase == nil {
		return nil, fmt.Errorf("no common ancestor found")
	}

	return mergeBase, nil
}

// getFirstLine extracts the first line from a multi-line message
func getFirstLine(message string) string {
	lines := splitLines(message)
	if len(lines) > 0 {
		return lines[0]
	}
	return message
}

// GetBranchCompareDiffs gets both staged and unstaged changes for branch comparison
func (gs *GitService) GetBranchCompareDiffs(logger *Logger) ([]FileDiff, []FileDiff, error) {
	// Get staged changes
	staged, err := gs.GetDiff(Staged, DiffOnly, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get staged changes: %w", err)
	}

	// Get unstaged changes
	unstaged, err := gs.GetDiff(Unstaged, DiffOnly, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get unstaged changes: %w", err)
	}

	return staged, unstaged, nil
}
