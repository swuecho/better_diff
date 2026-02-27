package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var errStopCommitIteration = errors.New("stop commit iteration")

// branchRefCandidates returns possible reference names for a branch
func branchRefCandidates(branch string) []plumbing.ReferenceName {
	return []plumbing.ReferenceName{
		plumbing.ReferenceName("refs/heads/" + branch),
		plumbing.ReferenceName("refs/remotes/origin/" + branch),
	}
}

// findBranchReference finds a branch reference by name
func (gs *GitService) findBranchReference(branch string) (*plumbing.Reference, error) {
	var lastErr error
	for _, refName := range branchRefCandidates(branch) {
		ref, err := gs.repo.Reference(refName, false)
		if err == nil && ref != nil {
			return ref, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("branch not found: %s", branch)
	}
	return nil, lastErr
}

// GetDefaultBranch returns the default branch name (main or master)
func (gs *GitService) GetDefaultBranch() (string, error) {
	for _, branch := range []string{"main", "master", "develop"} {
		if _, err := gs.findBranchReference(branch); err == nil {
			return branch, nil
		}
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

	currentCommit, defaultCommit, err := gs.resolveBranchHeadCommits(currentBranch, defaultBranch)
	if err != nil {
		return nil, err
	}

	// Get merge base
	mergeBase, err := getMergeBase(currentCommit, defaultCommit)
	if err != nil {
		return nil, fmt.Errorf("failed to get merge base: %w", err)
	}

	commits, foundMergeBase, err := gs.collectCommitsAhead(currentCommit, mergeBase)
	if err != nil {
		return nil, err
	}

	if !foundMergeBase && len(commits) > 50 {
		// Branches diverged and merge base was not reached in traversal.
		return commits[:50], nil
	}
	return commits, nil
}

// resolveBranchHeadCommits resolves the head commits for two branches
func (gs *GitService) resolveBranchHeadCommits(currentBranch, defaultBranch string) (*object.Commit, *object.Commit, error) {
	// Get current branch reference.
	currentRef, err := gs.repo.Reference(plumbing.ReferenceName("refs/heads/"+currentBranch), true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current branch reference: %w", err)
	}

	// Get default branch reference.
	defaultRef, err := gs.findBranchReference(defaultBranch)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get default branch reference: %w", err)
	}

	currentCommit, err := gs.repo.CommitObject(currentRef.Hash())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current commit: %w", err)
	}

	defaultCommit, err := gs.repo.CommitObject(defaultRef.Hash())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get default commit: %w", err)
	}

	return currentCommit, defaultCommit, nil
}

// collectCommitsAhead collects commits ahead of a merge base
func (gs *GitService) collectCommitsAhead(currentCommit, mergeBase *object.Commit) ([]Commit, bool, error) {
	// Get commits from merge base to current head (exclusive of merge base).
	commitIter, err := gs.repo.Log(&git.LogOptions{
		From:  currentCommit.Hash,
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to get commit log: %w", err)
	}

	var commits []Commit
	foundMergeBase := false

	iterateErr := commitIter.ForEach(func(c *object.Commit) error {
		if c.Hash.String() == mergeBase.Hash.String() {
			// Stop when we reach the merge base
			foundMergeBase = true
			return errStopCommitIteration
		}

		commits = append(commits, commitToSummary(c))

		return nil
	})

	if iterateErr != nil && !errors.Is(iterateErr, errStopCommitIteration) {
		return nil, false, iterateErr
	}

	return commits, foundMergeBase, nil
}

// commitToSummary converts a commit to a summary
func commitToSummary(commit *object.Commit) Commit {
	hash := commit.Hash.String()
	shortHash := hash
	if len(hash) > 7 {
		shortHash = hash[:7]
	}

	return Commit{
		Hash:      hash,
		ShortHash: shortHash,
		Author:    commit.Author.Name,
		Message:   getFirstLine(commit.Message),
		Date:      commit.Author.When.Format("2006-01-02 15:04"),
	}
}

// getMergeBase finds the merge base of two commits using go-git's graph algorithm.
func getMergeBase(commit1, commit2 *object.Commit) (*object.Commit, error) {
	bases, err := commit1.MergeBase(commit2)
	if err != nil {
		return nil, err
	}
	if len(bases) == 0 {
		return nil, fmt.Errorf("no common ancestor found")
	}

	// If multiple merge bases exist, prefer the most recent one for deterministic behavior.
	best := bases[0]
	for _, c := range bases[1:] {
		if c.Author.When.After(best.Author.When) {
			best = c
		}
	}
	return best, nil
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
	if logger == nil {
		return nil, nil, fmt.Errorf("logger is required")
	}

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

// GetUnifiedBranchCompareDiff returns a single unified diff per file:
// default branch tip (main/master) vs current working tree state.
func (gs *GitService) GetUnifiedBranchCompareDiff(viewMode DiffViewMode, contextLines int, logger *Logger) ([]FileDiff, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	worktree, baseCommit, paths, effectiveContext, err := gs.branchCompareInputs(viewMode, contextLines, logger)
	if err != nil {
		if errors.Is(err, errSkipBranchCompare) {
			return []FileDiff{}, nil
		}
		return nil, err
	}

	files := make([]FileDiff, 0, len(paths))
	for _, path := range paths {
		fileDiff, err := gs.buildUnifiedBranchCompareFileDiff(path, baseCommit, worktree, effectiveContext, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to compute unified diff for %s: %w", path, err)
		}
		if fileDiff == nil {
			continue
		}
		files = append(files, *fileDiff)
	}

	return files, nil
}

var errSkipBranchCompare = errors.New("skip branch compare")

// branchCompareInputs gathers inputs for branch compare operations
func (gs *GitService) branchCompareInputs(viewMode DiffViewMode, contextLines int, logger *Logger) (*git.Worktree, *object.Commit, []string, int, error) {
	worktree, err := gs.repo.Worktree()
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to get worktree: %w", err)
	}

	headCommit, shouldSkip, err := gs.getHeadCommitForBranchCompare(logger)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	if shouldSkip {
		return nil, nil, nil, 0, errSkipBranchCompare
	}

	baseCommit, err := gs.getDefaultBranchCommit()
	if err != nil {
		if isObjectNotFoundError(err) {
			logger.Warn("skip branch compare: default branch commit unavailable", nil)
			return nil, nil, nil, 0, errSkipBranchCompare
		}
		return nil, nil, nil, 0, fmt.Errorf("failed to resolve default branch commit: %w", err)
	}

	paths, err := gs.collectBranchComparePaths(baseCommit, headCommit, worktree)
	if err != nil {
		return nil, nil, nil, 0, err
	}

	return worktree, baseCommit, paths, resolveEffectiveContextLines(viewMode, contextLines), nil
}

// getHeadCommitForBranchCompare gets the HEAD commit for branch compare
func (gs *GitService) getHeadCommitForBranchCompare(logger *Logger) (*object.Commit, bool, error) {
	headRef, err := gs.repo.Head()
	if err != nil {
		if isObjectNotFoundError(err) {
			logger.Warn("skip branch compare: HEAD not available", nil)
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("failed to get HEAD: %w", err)
	}

	headCommit, err := gs.repo.CommitObject(headRef.Hash())
	if err != nil {
		if isObjectNotFoundError(err) {
			logger.Warn("skip branch compare: HEAD commit object missing", nil)
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	return headCommit, false, nil
}

// resolveEffectiveContextLines resolves context lines for branch compare
func resolveEffectiveContextLines(viewMode DiffViewMode, contextLines int) int {
	if viewMode == WholeFile {
		return WholeFileContext
	}
	return contextLines
}

// resolveBranchCompareChangeType determines change type from existence flags
func resolveBranchCompareChangeType(oldExists, newExists bool) ChangeType {
	if !oldExists && newExists {
		return Added
	}
	if oldExists && !newExists {
		return Deleted
	}
	return Modified
}

// buildUnifiedBranchCompareFileDiff builds a unified diff for a file in branch compare
func (gs *GitService) buildUnifiedBranchCompareFileDiff(path string, baseCommit *object.Commit, worktree *git.Worktree, contextLines int, logger *Logger) (*FileDiff, error) {
	oldContent, oldExists, err := gs.readFileFromCommit(baseCommit, path, logger)
	if err != nil {
		logger.Error("skip file in branch compare: read base content", err, map[string]any{
			"file": path,
		})
		return nil, nil
	}

	newContent, newExists, err := gs.readFileFromWorktree(worktree, path, logger)
	if err != nil {
		logger.Error("skip file in branch compare: read worktree content", err, map[string]any{
			"file": path,
		})
		return nil, nil
	}

	if !oldExists && !newExists {
		return nil, nil
	}

	hunks, err := buildUnifiedBranchCompareHunks(oldContent, newContent, contextLines)
	if err != nil {
		return nil, err
	}
	if len(hunks) == 0 {
		return nil, nil
	}

	linesAdded, linesRemoved := countHunkLineStats(hunks)
	return &FileDiff{
		Path:         path,
		ChangeType:   resolveBranchCompareChangeType(oldExists, newExists),
		Hunks:        hunks,
		LinesAdded:   linesAdded,
		LinesRemoved: linesRemoved,
	}, nil
}

// buildUnifiedBranchCompareHunks builds hunks for branch compare
func buildUnifiedBranchCompareHunks(oldContent, newContent []byte, contextLines int) ([]Hunk, error) {
	return computeHunksWithContext(splitLines(string(oldContent)), splitLines(string(newContent)), contextLines)
}

// getDefaultBranchCommit gets the commit for the default branch
func (gs *GitService) getDefaultBranchCommit() (*object.Commit, error) {
	defaultBranch, err := gs.GetDefaultBranch()
	if err != nil {
		return nil, err
	}

	ref, err := gs.findBranchReference(defaultBranch)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch reference: %w", err)
	}

	commit, err := gs.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch commit: %w", err)
	}
	return commit, nil
}

// collectBranchComparePaths collects paths for branch compare
func (gs *GitService) collectBranchComparePaths(baseCommit, headCommit *object.Commit, worktree *git.Worktree) ([]string, error) {
	pathSet := make(map[string]struct{})

	patch, patchErr := baseCommit.Patch(headCommit)
	if patchErr != nil {
		if !isObjectNotFoundError(patchErr) {
			return nil, fmt.Errorf("failed to compute base..HEAD patch: %w", patchErr)
		}
		// Fall back to status-only paths when commit patching cannot be computed.
	} else {
		addPathsFromFilePatches(pathSet, patch.FilePatches())
	}

	status, err := worktree.Status()
	if err != nil {
		if isObjectNotFoundError(err) {
			return sortedPathsFromSet(pathSet), nil
		}
		return nil, fmt.Errorf("failed to get worktree status: %w", err)
	}
	addChangedStatusPaths(pathSet, status)

	return sortedPathsFromSet(pathSet), nil
}

// addPathsFromFilePatches adds paths from file patches to a set
func addPathsFromFilePatches(pathSet map[string]struct{}, filePatches []diff.FilePatch) {
	for _, filePatch := range filePatches {
		from, to := filePatch.Files()
		addPathIfNotEmpty(pathSet, from)
		addPathIfNotEmpty(pathSet, to)
	}
}

// addPathIfNotEmpty adds a path to the set if the file is not nil/empty
func addPathIfNotEmpty(pathSet map[string]struct{}, file diff.File) {
	if file == nil || file.Path() == "" {
		return
	}
	pathSet[file.Path()] = struct{}{}
}

// addChangedStatusPaths adds changed paths from git status to a set
func addChangedStatusPaths(pathSet map[string]struct{}, status git.Status) {
	for path, fileStatus := range status {
		if fileStatus.Staging == git.Unmodified && fileStatus.Worktree == git.Unmodified && !status.IsUntracked(path) {
			continue
		}
		pathSet[path] = struct{}{}
	}
}

// readFileFromCommit reads a file from a commit
func (gs *GitService) readFileFromCommit(commit *object.Commit, path string, logger *Logger) ([]byte, bool, error) {
	file, err := commit.File(path)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get file %s from base commit: %w", path, err)
	}

	reader, err := file.Reader()
	if err != nil {
		return nil, false, fmt.Errorf("failed to open file %s from base commit: %w", path, err)
	}
	content, err := readAll(reader)
	reader.Close()
	if err != nil {
		return nil, false, fmt.Errorf("failed to read file %s from base commit: %w", path, err)
	}
	if err := enforceSizeLimit(path, content, logger, "Base file too large to diff", "file %s too large to diff (%d > %d)"); err != nil {
		return nil, false, err
	}
	return content, true, nil
}

// readFileFromWorktree reads a file from the worktree
func (gs *GitService) readFileFromWorktree(worktree *git.Worktree, path string, logger *Logger) ([]byte, bool, error) {
	file, err := worktree.Filesystem.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to open file %s from worktree: %w", path, err)
	}
	content, err := readAll(file)
	file.Close()
	if err != nil {
		return nil, false, fmt.Errorf("failed to read file %s from worktree: %w", path, err)
	}
	if err := enforceSizeLimit(path, content, logger, "Worktree file too large to diff", "file %s too large to diff (%d > %d)"); err != nil {
		return nil, false, err
	}
	return content, true, nil
}

// GetCommitDiff gets the diff for a specific commit (compares commit to its parent)
func (gs *GitService) GetCommitDiff(commitHash string, logger *Logger) ([]FileDiff, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	commit, parentCommit, err := gs.resolveCommitAndParent(commitHash)
	if err != nil {
		if errors.Is(err, object.ErrParentNotFound) {
			// First commit has no parent, so there is no parent-based diff.
			return []FileDiff{}, nil
		}
		return nil, err
	}

	patch, err := parentCommit.Patch(commit)
	if err != nil {
		return nil, fmt.Errorf("failed to get patch: %w", err)
	}

	logger.Info("got patch from git", map[string]any{
		"commit":       commitHash[:7],
		"file_patches": len(patch.FilePatches()),
	})

	return convertPatchToFileDiffs(patch.FilePatches(), logger, commitHash), nil
}

// resolveCommitAndParent resolves a commit and its parent
func (gs *GitService) resolveCommitAndParent(commitHash string) (*object.Commit, *object.Commit, error) {
	commit, err := gs.repo.CommitObject(plumbing.NewHash(commitHash))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get commit: %w", err)
	}

	if len(commit.ParentHashes) == 0 {
		return nil, nil, object.ErrParentNotFound
	}

	parentCommit, err := gs.repo.CommitObject(commit.ParentHashes[0])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get parent commit: %w", err)
	}

	return commit, parentCommit, nil
}

// convertPatchToFileDiffs converts file patches to FileDiff slices
func convertPatchToFileDiffs(filePatches []diff.FilePatch, logger *Logger, commitHash string) []FileDiff {
	files := make([]FileDiff, 0, len(filePatches))

	for _, filePatch := range filePatches {
		path := filePatchPath(filePatch)
		hunks, err := convertPatchToHunks(filePatch)
		if err != nil {
			logger.Warn("convert patch to hunks", map[string]any{
				"file":  path,
				"error": err,
			})
			continue
		}

		if len(hunks) == 0 {
			continue
		}

		linesAdded, linesRemoved := countHunkLineStats(hunks)
		logger.Info("processed file patch", map[string]any{
			"path":          path,
			"hunks":         len(hunks),
			"lines_added":   linesAdded,
			"lines_removed": linesRemoved,
		})

		files = append(files, FileDiff{
			Path:         path,
			ChangeType:   Modified,
			Hunks:        hunks,
			LinesAdded:   linesAdded,
			LinesRemoved: linesRemoved,
		})
	}

	logger.Info("loaded commit diff", map[string]any{
		"commit":     commitHash[:7],
		"file_count": len(files),
	})

	return files
}

// filePatchPath extracts the path from a file patch
func filePatchPath(filePatch diff.FilePatch) string {
	from, to := filePatch.Files()
	if to != nil && to.Path() != "" {
		return to.Path()
	}
	if from != nil && from.Path() != "" {
		return from.Path()
	}
	return "unknown"
}

// convertPatchToHunks converts a git Patch to our Hunk format
func convertPatchToHunks(filePatch diff.FilePatch) ([]Hunk, error) {
	chunks := filePatch.Chunks()
	if len(chunks) == 0 {
		return []Hunk{}, nil
	}

	var hunks []Hunk

	for _, chunk := range chunks {
		content := chunk.Content()
		if content == "" {
			continue
		}

		lines := splitLines(content)

		var currentHunk *Hunk
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}

			if len(line) > 2 && line[0] == '@' && line[1] == '@' {
				if currentHunk != nil && len(currentHunk.Lines) > 0 {
					hunks = append(hunks, *currentHunk)
				}
				currentHunk = &Hunk{
					Lines: []DiffLine{},
				}
				continue
			}

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
				continue
			}

			if currentHunk == nil {
				currentHunk = &Hunk{
					Lines: []DiffLine{},
				}
			}

			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				Type:       lineType,
				Content:    contentStr,
				OldLineNum: 0,
				NewLineNum: 0,
			})
		}

		if currentHunk != nil && len(currentHunk.Lines) > 0 {
			hunks = append(hunks, *currentHunk)
		}
	}

	return hunks, nil
}
