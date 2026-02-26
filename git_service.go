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

// sortStrings sorts a slice of strings in place
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

	// Collect paths and sort them for stable ordering
	paths := make([]string, 0, len(status))
	for path := range status {
		paths = append(paths, path)
	}
	sortStrings(paths)

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
	worktree, err := gs.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	var headCommit *object.Commit
	if mode == Staged {
		// Staged mode needs HEAD for index-vs-HEAD comparisons.
		head, err := gs.repo.Head()
		if err != nil {
			if isObjectNotFoundError(err) {
				if logger != nil {
					logger.Warn("Skipping staged diff load: HEAD not available", nil)
				}
				return []FileDiff{}, nil
			}
			return nil, fmt.Errorf("failed to get HEAD: %w", err)
		}

		headCommit, err = gs.repo.CommitObject(head.Hash())
		if err != nil {
			if isObjectNotFoundError(err) {
				if logger != nil {
					logger.Warn("Skipping staged diff load: HEAD commit object missing", nil)
				}
				return []FileDiff{}, nil
			}
			return nil, fmt.Errorf("failed to get HEAD commit: %w", err)
		}
	}

	// Get status to find changed files
	status, err := worktree.Status()
	if err != nil {
		if isObjectNotFoundError(err) {
			if logger != nil {
				logger.Warn("Skipping diff load: git status unavailable", nil)
			}
			return []FileDiff{}, nil
		}
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
		if !isRelevantChange(mode, status, path, fileStatus) {
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
	// Check if this is an untracked file (only in unstaged mode)
	isUntracked := mode == Unstaged && (fileStatus.Worktree == git.Untracked)

	var oldContent, newContent []byte
	var err error
	changeType := statusCodeToChangeType(statusCodeForMode(mode, fileStatus))

	if isUntracked {
		// For untracked files, show full file content as "new"
		changeType = Added
		newContent, err = gs.readRequiredWorktreeContent(path, worktree, logger)
		if err != nil {
			return nil, err
		}
	} else if mode == Staged {
		oldContent, newContent, err = gs.readStagedContents(path, idx, headCommit, logger)
		if err != nil {
			return nil, err
		}
	} else {
		oldContent, newContent, err = gs.readUnstagedContents(path, idx, worktree, logger)
		if err != nil {
			return nil, err
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

	fileDiff.LinesAdded, fileDiff.LinesRemoved = countHunkLineStats(hunks)

	return fileDiff, nil
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
	if logger != nil {
		logger.Warn(warnMsg, map[string]interface{}{
			"file": path,
			"size": len(content),
			"max":  MaxFileSize,
		})
	}
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

	// Get current branch reference
	currentRef, err := gs.repo.Reference(plumbing.ReferenceName("refs/heads/"+currentBranch), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get current branch reference: %w", err)
	}

	// Get default branch reference.
	defaultRef, err := gs.findBranchReference(defaultBranch)
	if err != nil {
		return nil, fmt.Errorf("failed to get default branch reference: %w", err)
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
	mergeBase, err := getMergeBase(currentCommit, defaultCommit)
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
			return errStopCommitIteration
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

	if err != nil && !errors.Is(err, errStopCommitIteration) {
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
	worktree, err := gs.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	headRef, err := gs.repo.Head()
	if err != nil {
		if isObjectNotFoundError(err) {
			if logger != nil {
				logger.Warn("Skipping branch compare: HEAD not available", nil)
			}
			return []FileDiff{}, nil
		}
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}
	headCommit, err := gs.repo.CommitObject(headRef.Hash())
	if err != nil {
		if isObjectNotFoundError(err) {
			if logger != nil {
				logger.Warn("Skipping branch compare: HEAD commit object missing", nil)
			}
			return []FileDiff{}, nil
		}
		return nil, fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	baseCommit, err := gs.getDefaultBranchCommit()
	if err != nil {
		if isObjectNotFoundError(err) {
			if logger != nil {
				logger.Warn("Skipping branch compare: default branch commit unavailable", nil)
			}
			return []FileDiff{}, nil
		}
		return nil, fmt.Errorf("failed to resolve default branch commit: %w", err)
	}

	paths, err := gs.collectBranchComparePaths(baseCommit, headCommit, worktree)
	if err != nil {
		return nil, err
	}

	effectiveContext := contextLines
	if viewMode == WholeFile {
		effectiveContext = WholeFileContext
	}

	files := make([]FileDiff, 0, len(paths))
	for _, path := range paths {
		oldContent, oldExists, err := gs.readFileFromCommit(baseCommit, path, logger)
		if err != nil {
			if logger != nil {
				logger.Error("Skipping file in branch compare: failed to read base content", err, map[string]interface{}{
					"file": path,
				})
			}
			continue
		}
		newContent, newExists, err := gs.readFileFromWorktree(worktree, path, logger)
		if err != nil {
			if logger != nil {
				logger.Error("Skipping file in branch compare: failed to read worktree content", err, map[string]interface{}{
					"file": path,
				})
			}
			continue
		}

		if !oldExists && !newExists {
			continue
		}

		hunks, err := computeHunksWithContext(splitLines(string(oldContent)), splitLines(string(newContent)), effectiveContext)
		if err != nil {
			return nil, fmt.Errorf("failed to compute unified diff for %s: %w", path, err)
		}
		if len(hunks) == 0 {
			continue
		}

		changeType := Modified
		if !oldExists && newExists {
			changeType = Added
		} else if oldExists && !newExists {
			changeType = Deleted
		}

		linesAdded, linesRemoved := countHunkLineStats(hunks)

		files = append(files, FileDiff{
			Path:         path,
			ChangeType:   changeType,
			Hunks:        hunks,
			LinesAdded:   linesAdded,
			LinesRemoved: linesRemoved,
		})
	}

	return files, nil
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

	patch, err := baseCommit.Patch(headCommit)
	if err != nil {
		if !isObjectNotFoundError(err) {
			return nil, fmt.Errorf("failed to compute base..HEAD patch: %w", err)
		}
		// Fall back to status-only paths when commit patching cannot be computed.
	} else {
		for _, filePatch := range patch.FilePatches() {
			from, to := filePatch.Files()
			if from != nil && from.Path() != "" {
				pathSet[from.Path()] = struct{}{}
			}
			if to != nil && to.Path() != "" {
				pathSet[to.Path()] = struct{}{}
			}
		}
	}

	status, err := worktree.Status()
	if err != nil {
		if isObjectNotFoundError(err) {
			paths := make([]string, 0, len(pathSet))
			for path := range pathSet {
				paths = append(paths, path)
			}
			sortStrings(paths)
			return paths, nil
		}
		return nil, fmt.Errorf("failed to get worktree status: %w", err)
	}
	for path, fileStatus := range status {
		if fileStatus.Staging != git.Unmodified || fileStatus.Worktree != git.Unmodified || status.IsUntracked(path) {
			pathSet[path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sortStrings(paths)
	return paths, nil
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
	if len(content) > MaxFileSize {
		if logger != nil {
			logger.Warn("Base file too large to diff", map[string]interface{}{
				"file": path,
				"size": len(content),
				"max":  MaxFileSize,
			})
		}
		return nil, false, fmt.Errorf("file %s too large to diff (%d > %d)", path, len(content), MaxFileSize)
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
	if len(content) > MaxFileSize {
		if logger != nil {
			logger.Warn("Worktree file too large to diff", map[string]interface{}{
				"file": path,
				"size": len(content),
				"max":  MaxFileSize,
			})
		}
		return nil, false, fmt.Errorf("file %s too large to diff (%d > %d)", path, len(content), MaxFileSize)
	}
	return content, true, nil
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

		linesAdded, linesRemoved := countHunkLineStats(hunks)

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
