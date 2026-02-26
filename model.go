package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Panel represents which panel is active
type Panel int

const (
	FileTreePanel Panel = iota
	DiffPanel
)

// Model holds the application state
type Model struct {
	git            *GitService // Git service (dependency injection)
	logger         *Logger     // Logger for error tracking
	watcher        *Watcher    // File system watcher
	files          []FileDiff
	diffFiles      []FileDiff // Files with full diff content
	fileTree       []TreeNode
	commits        []Commit // Commits ahead of main branch
	selectedCommit *Commit  // Currently selected commit in branch compare mode
	selectedIndex  int
	panel          Panel
	diffMode       DiffMode
	diffViewMode   DiffViewMode // Diff view mode (diff-only or whole file)
	scrollOffset   int          // For file tree scrolling
	diffScroll     int          // For diff panel scrolling
	width          int
	height         int
	rootPath       string
	branch         string
	quitting       bool
	showHelp       bool // Help modal visibility
	err            error
	lastFileHash   string // To detect changes in files
	vimPendingG    bool   // Tracks first "g" for "gg" in whole-file navigation
	diffContext    int    // Context lines in Diff Only mode
}

const gitServiceNotInitializedMessage = "Git service not initialized"

// TreeNode represents a node in the file tree
type TreeNode struct {
	name         string
	path         string
	isDir        bool
	isExpanded   bool
	children     []TreeNode
	changeType   ChangeType
	depth        int
	linesAdded   int
	linesRemoved int
}

// NewModel creates a new model with GitService and Logger
func NewModel(gitService *GitService, logger *Logger) Model {
	return Model{
		git:          gitService,
		logger:       logger,
		panel:        FileTreePanel,
		diffMode:     Unstaged,
		diffViewMode: DiffOnly,
		diffContext:  DefaultDiffContext,
		scrollOffset: 0,
		diffScroll:   0,
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.LoadGitInfo(),
		m.LoadFiles(),
		m.LoadAllDiffs(),
	)
}

func (m Model) ensureGitService() error {
	if m.git != nil {
		return nil
	}
	return &ServiceError{Message: gitServiceNotInitializedMessage}
}

func (m Model) logAndWrapError(message string, err error, fields map[string]any) tea.Msg {
	if m.logger != nil {
		m.logger.Error(message, err, fields)
	}
	return errMsg{err}
}

func findFileDiffByPath(files []FileDiff, path string) (FileDiff, bool) {
	for _, file := range files {
		if file.Path == path {
			return file, true
		}
	}
	return FileDiff{}, false
}

// LoadGitInfo loads git repository info
func (m Model) LoadGitInfo() tea.Cmd {
	return func() tea.Msg {
		if err := m.ensureGitService(); err != nil {
			return errMsg{err}
		}

		rootPath, err := m.git.GetRootPath()
		if err != nil {
			return m.logAndWrapError("Failed to get root path", err, nil)
		}
		branch, err := m.git.GetCurrentBranch()
		if err != nil {
			return m.logAndWrapError("Failed to get current branch", err, nil)
		}
		return gitInfoMsg{rootPath, branch}
	}
}

// LoadFiles loads changed files
func (m Model) LoadFiles() tea.Cmd {
	return func() tea.Msg {
		if err := m.ensureGitService(); err != nil {
			return errMsg{err}
		}

		files, err := m.git.GetChangedFiles(m.diffMode)
		if err != nil {
			return m.logAndWrapError("Failed to get changed files", err, map[string]any{
				"mode": m.diffMode,
			})
		}
		return filesLoadedMsg{files}
	}
}

// LoadDiff loads the diff for a specific file
func (m Model) LoadDiff(path string) tea.Cmd {
	return func() tea.Msg {
		if err := m.ensureGitService(); err != nil {
			return errMsg{err}
		}

		files, err := m.git.GetDiffWithContext(m.diffMode, m.diffViewMode, m.diffContext, m.logger)
		if err != nil {
			return m.logAndWrapError("Failed to get diff", err, map[string]any{
				"file": path,
				"mode": m.diffMode,
			})
		}
		file, found := findFileDiffByPath(files, path)
		if found {
			return diffLoadedMsg{file}
		}
		return diffLoadedMsg{FileDiff{}}
	}
}

// LoadAllDiffs loads diffs for all changed files at startup
func (m Model) LoadAllDiffs() tea.Cmd {
	return func() tea.Msg {
		if err := m.ensureGitService(); err != nil {
			return errMsg{err}
		}

		files, err := m.git.GetDiffWithContext(m.diffMode, m.diffViewMode, m.diffContext, m.logger)
		if err != nil {
			return m.logAndWrapError("Failed to get all diffs", err, map[string]any{
				"mode": m.diffMode,
			})
		}
		return allDiffsLoadedMsg{files}
	}
}

// LoadCommitsAhead loads commits ahead of main branch
func (m Model) LoadCommitsAhead() tea.Cmd {
	return func() tea.Msg {
		if err := m.ensureGitService(); err != nil {
			return errMsg{err}
		}

		commits, err := m.git.GetCommitsAheadOfMain()
		if err != nil {
			return m.logAndWrapError("Failed to get commits ahead", err, nil)
		}
		return commitsLoadedMsg{commits}
	}
}

// LoadCommitDiff loads the diff for a specific commit
func (m Model) LoadCommitDiff(commitHash string) tea.Cmd {
	return func() tea.Msg {
		if err := m.ensureGitService(); err != nil {
			return errMsg{err}
		}

		files, err := m.git.GetCommitDiff(commitHash, m.logger)
		if err != nil {
			return m.logAndWrapError("Failed to get commit diff", err, map[string]any{
				"commit": commitHash,
			})
		}
		return allDiffsLoadedMsg{files}
	}
}

// LoadBranchCompareDiff loads a unified diff against default branch.
func (m Model) LoadBranchCompareDiff(commits []Commit) tea.Cmd {
	return func() tea.Msg {
		if err := m.ensureGitService(); err != nil {
			return errMsg{err}
		}

		files, err := m.git.GetUnifiedBranchCompareDiff(m.diffViewMode, m.diffContext, m.logger)
		if err != nil {
			return m.logAndWrapError("Failed to get unified branch compare diff", err, map[string]any{
				"commit_count": len(commits),
			})
		}

		return allDiffsLoadedMsg{files}
	}
}

// ServiceError represents an error from a service not being initialized
type ServiceError struct {
	Message string
}

func (e *ServiceError) Error() string {
	return e.Message
}

// Messages

type gitInfoMsg struct {
	rootPath string
	branch   string
}

type filesLoadedMsg struct {
	files []FileDiff
}

type diffLoadedMsg struct {
	file FileDiff
}

type allDiffsLoadedMsg struct {
	files []FileDiff
}

type commitsLoadedMsg struct {
	commits []Commit
}

type errMsg struct {
	err error
}

type clearErrorMsg struct{}

// filesChangedMsg indicates that files have changed and need reloading
type filesChangedMsg struct {
	files []FileDiff
	hash  string
}
