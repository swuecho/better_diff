package main

import (
	"fmt"
	"sort"

	"github.com/go-git/go-git/v5"
)

// sortStrings sorts a slice of strings in place
func sortStrings(items []string) {
	sort.Strings(items)
}

// sortedStatusPaths returns sorted paths from a git status
func sortedStatusPaths(status git.Status) []string {
	paths := make([]string, 0, len(status))
	for path := range status {
		paths = append(paths, path)
	}
	sortStrings(paths)
	return paths
}

// sortedPathsFromSet returns sorted paths from a path set
func sortedPathsFromSet(pathSet map[string]struct{}) []string {
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sortStrings(paths)
	return paths
}

// statusCodeForMode returns the status code for a file based on diff mode
func statusCodeForMode(mode DiffMode, fileStatus git.FileStatus) string {
	if mode == Staged {
		return string(fileStatus.Staging)
	}
	return string(fileStatus.Worktree)
}

// statusCodeToChangeType converts a git status code to a ChangeType
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

// isRelevantChange checks if a file change is relevant for the given mode
func isRelevantChange(mode DiffMode, status git.Status, path string, fileStatus *git.FileStatus) bool {
	if mode == Staged {
		return fileStatus.Staging != git.Unmodified && !status.IsUntracked(path)
	}
	return fileStatus.Worktree != git.Unmodified || status.IsUntracked(path)
}

// changeTypeForMode determines the change type for a file based on mode
func changeTypeForMode(mode DiffMode, status git.Status, path string, fileStatus *git.FileStatus) ChangeType {
	if mode == Unstaged && status.IsUntracked(path) {
		return Added
	}
	return statusCodeToChangeType(statusCodeForMode(mode, *fileStatus))
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
