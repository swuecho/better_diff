package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var errStopCommitIteration = errors.New("stop commit iteration")

func sortStrings(items []string) {
	sort.Strings(items)
}

func isObjectNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "object not found")
}

func statusCodeForMode(mode DiffMode, fileStatus git.FileStatus) string {
	if mode == Staged {
		return string(fileStatus.Staging)
	}
	return string(fileStatus.Worktree)
}

func statusCodeToChangeType(statusCode string) ChangeType {
	switch statusCode {
	case "M":
		return Modified
	case "A":
		return Added
	case "D":
		return Deleted
	case "R":
		return Renamed
	default:
		return Modified
	}
}

func isRelevantChange(mode DiffMode, status git.Status, path string, fileStatus *git.FileStatus) bool {
	if mode == Staged {
		return fileStatus.Staging != git.Unmodified && !status.IsUntracked(path)
	}
	return fileStatus.Worktree != git.Unmodified || status.IsUntracked(path)
}

func changeTypeForMode(mode DiffMode, status git.Status, path string, fileStatus *git.FileStatus) ChangeType {
	if mode == Unstaged && status.IsUntracked(path) {
		return Added
	}
	return statusCodeToChangeType(statusCodeForMode(mode, *fileStatus))
}

func sortedStatusPaths(status git.Status) []string {
	paths := make([]string, 0, len(status))
	for path := range status {
		paths = append(paths, path)
	}
	sortStrings(paths)
	return paths
}

func sortedPathsFromSet(pathSet map[string]struct{}) []string {
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sortStrings(paths)
	return paths
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
		if isObjectNotFoundError(err) {
			return []FileDiff{}, nil
		}
		return nil, fmt.Errorf("failed to get git status: %w", err)
	}

	paths := sortedStatusPaths(status)

	var files []FileDiff
	for _, path := range paths {
		fileStatus := status[path]
		if !isRelevantChange(mode, status, path, fileStatus) {
			continue
		}

		files = append(files, FileDiff{
			Path:         path,
			ChangeType:   changeTypeForMode(mode, status, path, fileStatus),
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

var errSkipDiffLoad = errors.New("skip diff load")

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

func effectiveContextLines(viewMode DiffViewMode, contextLines int) int {
	if viewMode == WholeFile {
		return WholeFileContext
	}
	return contextLines
}

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

func branchRefCandidates(branch string) []plumbing.ReferenceName {
	return []plumbing.ReferenceName{
		plumbing.ReferenceName("refs/heads/" + branch),
		plumbing.ReferenceName("refs/remotes/origin/" + branch),
	}
}

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

func resolveEffectiveContextLines(viewMode DiffViewMode, contextLines int) int {
	if viewMode == WholeFile {
		return WholeFileContext
	}
	return contextLines
}

func resolveBranchCompareChangeType(oldExists, newExists bool) ChangeType {
	if !oldExists && newExists {
		return Added
	}
	if oldExists && !newExists {
		return Deleted
	}
	return Modified
}

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

func buildUnifiedBranchCompareHunks(oldContent, newContent []byte, contextLines int) ([]Hunk, error) {
	return computeHunksWithContext(splitLines(string(oldContent)), splitLines(string(newContent)), contextLines)
}

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

func addPathsFromFilePatches(pathSet map[string]struct{}, filePatches []diff.FilePatch) {
	for _, filePatch := range filePatches {
		from, to := filePatch.Files()
		addPathIfNotEmpty(pathSet, from)
		addPathIfNotEmpty(pathSet, to)
	}
}

func addPathIfNotEmpty(pathSet map[string]struct{}, file diff.File) {
	if file == nil || file.Path() == "" {
		return
	}
	pathSet[file.Path()] = struct{}{}
}

func addChangedStatusPaths(pathSet map[string]struct{}, status git.Status) {
	for path, fileStatus := range status {
		if fileStatus.Staging == git.Unmodified && fileStatus.Worktree == git.Unmodified && !status.IsUntracked(path) {
			continue
		}
		pathSet[path] = struct{}{}
	}
}

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
				Type:    lineType,
				Content: contentStr,
				LineNum: 0,
			})
		}

		if currentHunk != nil && len(currentHunk.Lines) > 0 {
			hunks = append(hunks, *currentHunk)
		}
	}

	return hunks, nil
}
