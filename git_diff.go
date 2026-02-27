package main

import (
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var errSkipDiffLoad = errors.New("skip diff load")

// GetDiff gets the git diff based on mode with default context.
func (gs *GitService) GetDiff(mode DiffMode, viewMode DiffViewMode, logger *Logger) ([]FileDiff, error) {
	return gs.GetDiffWithContext(mode, viewMode, DefaultDiffContext, logger)
}

// GetDiffWithContext gets the git diff based on mode and configurable context lines.
func (gs *GitService) GetDiffWithContext(mode DiffMode, viewMode DiffViewMode, contextLines int, logger *Logger) ([]FileDiff, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	worktree, idx, status, headCommit, err := gs.diffInputs(mode, logger)
	if err != nil {
		if errors.Is(err, errSkipDiffLoad) {
			return []FileDiff{}, nil
		}
		return nil, err
	}

	paths := sortedStatusPaths(status)
	files := make([]FileDiff, 0, len(paths))
	for _, path := range paths {
		fileStatus := status[path]
		if !isRelevantChange(mode, status, path, fileStatus) {
			continue
		}

		fileDiff, err := gs.getFileDiff(worktree, idx, headCommit, path, mode, viewMode, contextLines, *fileStatus, logger)
		if err != nil {
			// Log error but continue with other files
			logger.Error("get file diff", err, map[string]any{
				"file": path,
				"mode": mode,
			})
			continue
		}

		if fileDiff != nil {
			files = append(files, *fileDiff)
		}
	}

	return files, nil
}

// diffInputs gathers the inputs needed for diff operations
func (gs *GitService) diffInputs(mode DiffMode, logger *Logger) (*git.Worktree, *index.Index, git.Status, *object.Commit, error) {
	worktree, err := gs.repo.Worktree()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	headCommit, shouldSkip, err := gs.getHeadCommitForDiffMode(mode, logger)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if shouldSkip {
		return nil, nil, nil, nil, errSkipDiffLoad
	}

	status, shouldSkip, err := getWorktreeStatusForDiff(worktree, logger)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if shouldSkip {
		return nil, nil, nil, nil, errSkipDiffLoad
	}

	idx, err := gs.repo.Storer.Index()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get index: %w", err)
	}

	return worktree, idx, status, headCommit, nil
}

// getHeadCommitForDiffMode gets the HEAD commit for staged diff mode
func (gs *GitService) getHeadCommitForDiffMode(mode DiffMode, logger *Logger) (*object.Commit, bool, error) {
	if mode != Staged {
		return nil, false, nil
	}

	head, err := gs.repo.Head()
	if err != nil {
		if isObjectNotFoundError(err) {
			logger.Warn("skip staged diff load: HEAD not available", nil)
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("failed to get HEAD: %w", err)
	}

	headCommit, err := gs.repo.CommitObject(head.Hash())
	if err != nil {
		if isObjectNotFoundError(err) {
			logger.Warn("skip staged diff load: HEAD commit object missing", nil)
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	return headCommit, false, nil
}

// getWorktreeStatusForDiff gets the worktree status for diff operations
func getWorktreeStatusForDiff(worktree *git.Worktree, logger *Logger) (git.Status, bool, error) {
	status, err := worktree.Status()
	if err != nil {
		if isObjectNotFoundError(err) {
			logger.Warn("skip diff load: git status unavailable", nil)
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("failed to get git status: %w", err)
	}

	return status, false, nil
}

// getFileDiff generates a FileDiff for a single file
func (gs *GitService) getFileDiff(worktree *git.Worktree, idx *index.Index, headCommit *object.Commit, path string, mode DiffMode, viewMode DiffViewMode, contextLines int, fileStatus git.FileStatus, logger *Logger) (*FileDiff, error) {
	changeType := statusCodeToChangeType(statusCodeForMode(mode, fileStatus))
	oldContent, newContent, resolvedChangeType, err := gs.loadDiffContents(path, mode, fileStatus, idx, headCommit, worktree, logger)
	if err != nil {
		return nil, err
	}
	changeType = resolvedChangeType

	hunks, err := computeHunksWithContext(
		splitLines(string(oldContent)),
		splitLines(string(newContent)),
		effectiveContextLines(viewMode, contextLines),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute diff for %s: %w", path, err)
	}

	linesAdded, linesRemoved := countHunkLineStats(hunks)
	return &FileDiff{
		Path:         path,
		ChangeType:   changeType,
		Hunks:        hunks,
		LinesAdded:   linesAdded,
		LinesRemoved: linesRemoved,
	}, nil
}

// effectiveContextLines returns the effective context lines based on view mode
func effectiveContextLines(viewMode DiffViewMode, contextLines int) int {
	if viewMode == WholeFile {
		return WholeFileContext
	}
	return contextLines
}

// loadDiffContents loads the old and new content for a file diff
func (gs *GitService) loadDiffContents(path string, mode DiffMode, fileStatus git.FileStatus, idx *index.Index, headCommit *object.Commit, worktree *git.Worktree, logger *Logger) ([]byte, []byte, ChangeType, error) {
	// Check if this is an untracked file (only in unstaged mode).
	isUntracked := mode == Unstaged && fileStatus.Worktree == git.Untracked
	if isUntracked {
		newContent, err := gs.readRequiredWorktreeContent(path, worktree, logger)
		if err != nil {
			return nil, nil, Modified, err
		}
		return nil, newContent, Added, nil
	}

	changeType := statusCodeToChangeType(statusCodeForMode(mode, fileStatus))
	if mode == Staged {
		oldContent, newContent, err := gs.readStagedContents(path, idx, headCommit, logger)
		if err != nil {
			return nil, nil, Modified, err
		}
		return oldContent, newContent, changeType, nil
	}

	oldContent, newContent, err := gs.readUnstagedContents(path, idx, worktree, logger)
	if err != nil {
		return nil, nil, Modified, err
	}
	return oldContent, newContent, changeType, nil
}

// readRequiredWorktreeContent reads content from worktree (for untracked files)
func (gs *GitService) readRequiredWorktreeContent(path string, worktree *git.Worktree, logger *Logger) ([]byte, error) {
	file, err := worktree.Filesystem.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open untracked file %s: %w", path, err)
	}
	content, err := readAll(file)
	file.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read untracked file %s: %w", path, err)
	}
	if err := enforceSizeLimit(path, content, logger, "Untracked file too large to display", "untracked file %s too large to display (%d > %d)"); err != nil {
		return nil, err
	}
	return content, nil
}

// readStagedContents reads old (HEAD) and new (index) content for staged changes
func (gs *GitService) readStagedContents(path string, idx *index.Index, headCommit *object.Commit, logger *Logger) ([]byte, []byte, error) {
	oldContent, err := gs.readHeadContentIfPresent(path, headCommit, logger)
	if err != nil {
		return nil, nil, err
	}
	newContent, err := gs.readIndexContentIfPresent(path, idx, logger, "failed to read new file %s from index: %w")
	if err != nil {
		return nil, nil, err
	}
	return oldContent, newContent, nil
}

// readUnstagedContents reads old (index) and new (worktree) content for unstaged changes
func (gs *GitService) readUnstagedContents(path string, idx *index.Index, worktree *git.Worktree, logger *Logger) ([]byte, []byte, error) {
	oldContent, err := gs.readIndexContentIfPresent(path, idx, logger, "failed to read old file %s from index: %w")
	if err != nil {
		return nil, nil, err
	}
	newContent, err := gs.readWorktreeContentIfPresent(path, worktree, logger)
	if err != nil {
		return nil, nil, err
	}
	return oldContent, newContent, nil
}

// readHeadContentIfPresent reads file content from HEAD commit
func (gs *GitService) readHeadContentIfPresent(path string, headCommit *object.Commit, logger *Logger) ([]byte, error) {
	if headCommit == nil {
		return nil, nil
	}
	oldFile, err := headCommit.File(path)
	if err != nil {
		return nil, nil
	}
	oldReader, err := oldFile.Reader()
	if err != nil {
		return nil, nil
	}
	content, err := readAll(oldReader)
	oldReader.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read old file %s: %w", path, err)
	}
	if err := enforceSizeLimit(path, content, logger, "File too large to diff", "file %s too large to diff (%d > %d)"); err != nil {
		return nil, err
	}
	return content, nil
}

// readIndexContentIfPresent reads file content from git index
func (gs *GitService) readIndexContentIfPresent(path string, idx *index.Index, logger *Logger, readErrFmt string) ([]byte, error) {
	for _, entry := range idx.Entries {
		if entry.Name != path {
			continue
		}

		blob, err := object.GetBlob(gs.repo.Storer, entry.Hash)
		if err != nil {
			return nil, nil
		}
		reader, err := blob.Reader()
		if err != nil {
			return nil, nil
		}

		content, err := readAll(reader)
		reader.Close()
		if err != nil {
			return nil, fmt.Errorf(readErrFmt, path, err)
		}
		if err := enforceSizeLimit(path, content, logger, "File too large to diff", "file %s too large to diff (%d > %d)"); err != nil {
			return nil, err
		}
		return content, nil
	}
	return nil, nil
}

// readWorktreeContentIfPresent reads file content from worktree
func (gs *GitService) readWorktreeContentIfPresent(path string, worktree *git.Worktree, logger *Logger) ([]byte, error) {
	file, err := worktree.Filesystem.Open(path)
	if err != nil {
		return nil, nil
	}
	content, err := readAll(file)
	file.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read new file %s from worktree: %w", path, err)
	}
	if err := enforceSizeLimit(path, content, logger, "File too large to diff", "file %s too large to diff (%d > %d)"); err != nil {
		return nil, err
	}
	return content, nil
}

// enforceSizeLimit checks if file content exceeds the size limit
func enforceSizeLimit(path string, content []byte, logger *Logger, warnMsg, errFmt string) error {
	if len(content) <= MaxFileSize {
		return nil
	}
	logger.Warn(warnMsg, map[string]any{
		"file": path,
		"size": len(content),
		"max":  MaxFileSize,
	})
	return fmt.Errorf(errFmt, path, len(content), MaxFileSize)
}

// countHunkLineStats counts added and removed lines in hunks
func countHunkLineStats(hunks []Hunk) (added int, removed int) {
	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			if line.Type == LineAdded {
				added++
			} else if line.Type == LineRemoved {
				removed++
			}
		}
	}
	return added, removed
}
