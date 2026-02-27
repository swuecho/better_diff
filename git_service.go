package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
)

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

// isObjectNotFoundError checks if an error is related to missing git objects
func isObjectNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "object not found")
}
