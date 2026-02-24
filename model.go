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
	files         []FileDiff
	diffFiles     []FileDiff // Files with full diff content
	fileTree      []TreeNode
	selectedIndex int
	panel         Panel
	diffMode      DiffMode
	scrollOffset  int // For file tree scrolling
	diffScroll    int // For diff panel scrolling
	width         int
	height        int
	rootPath      string
	branch        string
	quitting      bool
	err           error
}

// TreeNode represents a node in the file tree
type TreeNode struct {
	name      string
	path      string
	isDir     bool
	isExpanded bool
	children  []TreeNode
	changeType ChangeType
	depth     int
}

// NewModel creates a new model
func NewModel() Model {
	return Model{
		panel:        FileTreePanel,
		diffMode:     Unstaged,
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

// LoadGitInfo loads git repository info
func (m Model) LoadGitInfo() tea.Cmd {
	return func() tea.Msg {
		rootPath, err := GetRootPath()
		if err != nil {
			return errMsg{err}
		}
		branch, err := GetCurrentBranch()
		if err != nil {
			return errMsg{err}
		}
		return gitInfoMsg{rootPath, branch}
	}
}

// LoadFiles loads changed files
func (m Model) LoadFiles() tea.Cmd {
	return func() tea.Msg {
		files, err := GetChangedFiles(m.diffMode)
		if err != nil {
			return errMsg{err}
		}
		return filesLoadedMsg{files}
	}
}

// LoadDiff loads the diff for a specific file
func (m Model) LoadDiff(path string) tea.Cmd {
	return func() tea.Msg {
		files, err := GetDiff(m.diffMode)
		if err != nil {
			return errMsg{err}
		}
		// Find the file with matching path
		for _, f := range files {
			if f.Path == path {
				return diffLoadedMsg{f}
			}
		}
		return diffLoadedMsg{FileDiff{}}
	}
}

// LoadAllDiffs loads diffs for all changed files at startup
func (m Model) LoadAllDiffs() tea.Cmd {
	return func() tea.Msg {
		files, err := GetDiff(m.diffMode)
		if err != nil {
			return errMsg{err}
		}
		return allDiffsLoadedMsg{files}
	}
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

type errMsg struct {
	err error
}

type clearErrorMsg struct{}

// TickMsg is for periodic updates
type TickMsg struct {
	time int
}
