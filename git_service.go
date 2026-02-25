package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/diff"
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

// GetDiff gets the git diff based on mode with default context.
func (gs *GitService) GetDiff(mode DiffMode, viewMode DiffViewMode, logger *Logger) ([]FileDiff, error) {
	return gs.GetDiffWithContext(mode, viewMode, DefaultDiffContext, logger)
}

// GetDiffWithContext gets the git diff based on mode and configurable context lines.
func (gs *GitService) GetDiffWithContext(mode DiffMode, viewMode DiffViewMode, contextLines int, logger *Logger) ([]FileDiff, error) {
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

		fileDiff, err := gs.getFileDiff(worktree, idx, headCommit, path, mode, viewMode, contextLines, *fileStatus, logger)
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
func (gs *GitService) getFileDiff(worktree *git.Worktree, idx *index.Index, headCommit *object.Commit, path string, mode DiffMode, viewMode DiffViewMode, contextLines int, fileStatus git.FileStatus, logger *Logger) (*FileDiff, error) {
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

	effectiveContext := contextLines
	if viewMode == WholeFile {
		effectiveContext = WholeFileContext
	}

	hunks, err := computeHunksWithContext(oldLines, newLines, effectiveContext)
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

// GetBranchCompareDiffs gets both staged and unstaged changes for branch comparison.
func (gs *GitService) GetBranchCompareDiffs(viewMode DiffViewMode, contextLines int, logger *Logger) ([]FileDiff, []FileDiff, error) {
	// Get staged changes
	staged, err := gs.GetDiffWithContext(Staged, viewMode, contextLines, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get staged changes: %w", err)
	}

	// Get unstaged changes
	unstaged, err := gs.GetDiffWithContext(Unstaged, viewMode, contextLines, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get unstaged changes: %w", err)
	}

	return staged, unstaged, nil
}

// GetCommitDiff gets the diff for a specific commit (compares commit to its parent)
func (gs *GitService) GetCommitDiff(commitHash string, logger *Logger) ([]FileDiff, error) {
	// Get the commit object
	hash := plumbing.NewHash(commitHash)
	commit, err := gs.repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	// Get parent commit
	var parentCommit *object.Commit
	if len(commit.ParentHashes) > 0 {
		parentCommit, err = gs.repo.CommitObject(commit.ParentHashes[0])
		if err != nil {
			return nil, fmt.Errorf("failed to get parent commit: %w", err)
		}
	} else {
		// This is the first commit (no parent), return empty
		return []FileDiff{}, nil
	}

	// Get the patch as a string for debugging
	patch, err := parentCommit.Patch(commit)
	if err != nil {
		return nil, fmt.Errorf("failed to get patch: %w", err)
	}

	if logger != nil {
		logger.Info("Got patch from git", map[string]interface{}{
			"commit":       commitHash[:7],
			"file_patches": len(patch.FilePatches()),
		})
	}

	// Convert patch to FileDiffs
	var files []FileDiff
	for _, filePatch := range patch.FilePatches() {
		from, to := filePatch.Files()
		path := "unknown"
		changeType := Modified

		if from != nil {
			path = from.Path()
		}
		if to != nil {
			path = to.Path()
		}

		// Generate hunks from patch
		hunks, err := convertPatchToHunks(filePatch)
		if err != nil {
			if logger != nil {
				logger.Warn("Failed to convert patch to hunks", map[string]interface{}{
					"file":  path,
					"error": err,
				})
			}
			continue
		}

		// Skip files with no hunks (no actual changes)
		if len(hunks) == 0 {
			continue
		}

		// Count lines
		linesAdded := 0
		linesRemoved := 0
		for _, hunk := range hunks {
			for _, line := range hunk.Lines {
				if line.Type == LineAdded {
					linesAdded++
				} else if line.Type == LineRemoved {
					linesRemoved++
				}
			}
		}

		if logger != nil {
			logger.Info("Processed file patch", map[string]interface{}{
				"path":          path,
				"hunks":         len(hunks),
				"lines_added":   linesAdded,
				"lines_removed": linesRemoved,
			})
		}

		files = append(files, FileDiff{
			Path:         path,
			ChangeType:   changeType,
			Hunks:        hunks,
			LinesAdded:   linesAdded,
			LinesRemoved: linesRemoved,
		})
	}

	if logger != nil {
		logger.Info("Loaded commit diff", map[string]interface{}{
			"commit":     commitHash[:7],
			"file_count": len(files),
		})
	}

	return files, nil
}

// convertPatchToHunks converts a git Patch to our Hunk format
func convertPatchToHunks(filePatch diff.FilePatch) ([]Hunk, error) {
	// Get all chunks from the file patch
	chunks := filePatch.Chunks()

	// Debug: Log chunk count
	// fmt.Fprintf(os.Stderr, "DEBUG: FilePatch has %d chunks\n", len(chunks))

	// If no chunks, return empty
	if len(chunks) == 0 {
		return []Hunk{}, nil
	}

	var hunks []Hunk

	// Process each chunk
	for _, chunk := range chunks {
		// Get the content of this chunk
		content := chunk.Content()

		if content == "" {
			continue
		}

		// Split content into lines
		lines := splitLines(content)

		var currentHunk *Hunk
		for _, line := range lines {
			// Skip empty lines
			if len(line) == 0 {
				continue
			}

			// Check for hunk header (@@ -oldStart,oldCount +newStart,newCount @@)
			if len(line) > 2 && line[0] == '@' && line[1] == '@' {
				// Save previous hunk if exists
				if currentHunk != nil && len(currentHunk.Lines) > 0 {
					hunks = append(hunks, *currentHunk)
				}
				// Start new hunk
				currentHunk = &Hunk{
					Lines: []DiffLine{},
				}
				continue
			}

			// Parse diff lines
			var lineType LineType
			var contentStr string

			switch line[0] {
			case '+':
				lineType = LineAdded
				contentStr = line[1:]
			case '-':
				lineType = LineRemoved
				contentStr = line[1:]
			case ' ':
				lineType = LineContext
				contentStr = line[1:]
			default:
				// Skip any other lines (like file paths, etc.)
				continue
			}

			// Create hunk if needed
			if currentHunk == nil {
				currentHunk = &Hunk{
					Lines: []DiffLine{},
				}
			}

			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				Type:    lineType,
				Content: contentStr,
				LineNum: 0,
			})
		}

		// Save last hunk from this chunk
		if currentHunk != nil && len(currentHunk.Lines) > 0 {
			hunks = append(hunks, *currentHunk)
		}
	}

	// Debug: Log result
	// fmt.Fprintf(os.Stderr, "DEBUG: Converted to %d hunks\n", len(hunks))

	return hunks, nil
}
