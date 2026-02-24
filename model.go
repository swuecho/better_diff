package main

import (
	"time"

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
	diffViewMode  DiffViewMode // Diff view mode (diff-only or whole file)
	scrollOffset  int          // For file tree scrolling
	diffScroll    int          // For diff panel scrolling
	width         int
	height        int
	rootPath      string
	branch        string
	quitting      bool
	err           error
	lastFileHash  string // To detect changes in files
}

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

// NewModel creates a new model
func NewModel() Model {
	return Model{
		panel:        FileTreePanel,
		diffMode:     Unstaged,
		diffViewMode: DiffOnly,
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
		tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
			return TickMsg{time: t.Second()}
		}),
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
		files, err := GetDiff(m.diffMode, m.diffViewMode)
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
		files, err := GetDiff(m.diffMode, m.diffViewMode)
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

// filesChangedMsg indicates that files have changed and need reloading
type filesChangedMsg struct {
	files []FileDiff
	hash  string
}

// TickMsg is for periodic updates
type TickMsg struct {
	time int
}
