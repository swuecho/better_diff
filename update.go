package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// dirNode is used for building the file tree
type dirNode struct {
	path     string
	name     string
	files    []FileDiff
	subdirs  map[string]*dirNode
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.panel == DiffPanel {
				m.moveDiffUp()
			} else {
				m.moveUp()
			}
			return m, nil

		case "down", "j":
			if m.panel == DiffPanel {
				m.moveDiffDown()
			} else {
				m.moveDown()
			}
			return m, nil

		case "pgup":
			if m.panel == DiffPanel {
				m.moveDiffPageUp()
			} else {
				m.movePageUp()
			}
			return m, nil

		case "pgdown":
			if m.panel == DiffPanel {
				m.moveDiffPageDown()
			} else {
				m.movePageDown()
			}
			return m, nil

		case "tab":
			// Don't allow switching to file tree panel in whole file mode
			if m.diffViewMode == WholeFile {
				return m, nil
			}
			if m.panel == FileTreePanel {
				m.panel = DiffPanel
			} else {
				m.panel = FileTreePanel
			}
			return m, nil

		case "enter", " ":
			if m.panel == FileTreePanel {
				cmd := m.selectItem()
				return m, cmd
			}
			return m, nil

		case "s":
			// Toggle between staged and unstaged
			if m.diffMode == Unstaged {
				m.diffMode = Staged
			} else {
				m.diffMode = Unstaged
			}
			m.selectedIndex = 0
			m.scrollOffset = 0
			m.diffScroll = 0
			m.diffFiles = nil
			m.files = nil // Clear to force reload
			return m, m.LoadFiles()

		case "f":
			// Toggle between diff-only and whole file view
			if m.diffViewMode == DiffOnly {
				m.diffViewMode = WholeFile
				m.panel = DiffPanel // Auto-switch to diff panel
			} else {
				m.diffViewMode = DiffOnly
			}
			m.diffScroll = 0
			m.diffFiles = nil
			return m, m.LoadAllDiffs()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
			return TickMsg{time: t.Second()}
		})

	case TickMsg:
		// Periodically check for changes
		return m, m.checkForChanges()

	case gitInfoMsg:
		m.rootPath = msg.rootPath
		m.branch = msg.branch
		return m, nil

	case filesLoadedMsg:
		m.files = msg.files
		m.lastFileHash = computeFileHash(msg.files)
		m.buildFileTree()
		return m, nil

	case allDiffsLoadedMsg:
		m.diffFiles = msg.files
		return m, nil

	case filesChangedMsg:
		// Files have changed, reload everything
		m.files = msg.files
		m.lastFileHash = msg.hash
		m.buildFileTree()
		m.diffFiles = nil // Clear old diffs
		// Reload diffs
		return m, tea.Batch(
			m.LoadAllDiffs(),
			tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
				return TickMsg{time: t.Second()}
			}),
		)

	case diffLoadedMsg:
		// Only add if the file has actual content (not empty)
		if msg.file.Path != "" {
			// Check if file already exists and replace it, otherwise append
			found := false
			for i := range m.diffFiles {
				if m.diffFiles[i].Path == msg.file.Path {
					m.diffFiles[i] = msg.file
					found = true
					break
				}
			}
			if !found {
				m.diffFiles = append(m.diffFiles, msg.file)
			}
		}
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case clearErrorMsg:
		m.err = nil
		return m, nil
}

	return m, nil
}

// moveUp moves the selection up
func (m *Model) moveUp() {
	if m.selectedIndex > 0 {
		m.selectedIndex--

		// Auto scroll if needed
		if m.selectedIndex < m.scrollOffset {
			m.scrollOffset = m.selectedIndex
		}
	}
}

// moveDown moves the selection down
func (m *Model) moveDown() {
	if m.selectedIndex < len(m.flattenTree())-1 {
		m.selectedIndex++

		// Auto scroll if needed
		visibleHeight := m.height - 3 // minus header and footer
		if m.selectedIndex >= m.scrollOffset+visibleHeight {
			m.scrollOffset = m.selectedIndex - visibleHeight + 1
		}
	}
}

// movePageUp moves the selection up by a page
func (m *Model) movePageUp() {
	visibleHeight := m.height - 3
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	// Move selection up by a page
	m.selectedIndex -= visibleHeight
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}

	// Adjust scroll offset
	if m.selectedIndex < m.scrollOffset {
		m.scrollOffset = m.selectedIndex
	}
}

// movePageDown moves the selection down by a page
func (m *Model) movePageDown() {
	visibleHeight := m.height - 3
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	maxIndex := len(m.flattenTree()) - 1
	m.selectedIndex += visibleHeight
	if m.selectedIndex > maxIndex {
		m.selectedIndex = maxIndex
	}

	// Auto scroll if needed
	if m.selectedIndex >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.selectedIndex - visibleHeight + 1
	}
}

// moveDiffUp scrolls the diff view up
func (m *Model) moveDiffUp() {
	if m.diffScroll > 0 {
		m.diffScroll--
	}
}

// moveDiffDown scrolls the diff view down
func (m *Model) moveDiffDown() {
	// Get total diff line count
	totalLines := m.getDiffLineCount()
	visibleHeight := m.height - 3 // minus header and footer

	if m.diffScroll < totalLines-visibleHeight {
		m.diffScroll++
	}
}

// moveDiffPageUp scrolls the diff view up by a page
func (m *Model) moveDiffPageUp() {
	visibleHeight := m.height - 3
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	m.diffScroll -= visibleHeight
	if m.diffScroll < 0 {
		m.diffScroll = 0
	}
}

// moveDiffPageDown scrolls the diff view down by a page
func (m *Model) moveDiffPageDown() {
	visibleHeight := m.height - 3
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	totalLines := m.getDiffLineCount()
	m.diffScroll += visibleHeight
	if m.diffScroll > totalLines-visibleHeight {
		m.diffScroll = max(0, totalLines-visibleHeight)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// getDiffLineCount returns the total number of lines in the current diff
func (m *Model) getDiffLineCount() int {
	flatTree := m.flattenTree()
	if m.selectedIndex >= len(flatTree) {
		return 0
	}

	node := flatTree[m.selectedIndex]
	if node.isDir {
		return 0
	}

	for i := range m.diffFiles {
		if m.diffFiles[i].Path == node.path {
			count := 0
			for _, hunk := range m.diffFiles[i].Hunks {
				count += 2 + len(hunk.Lines) // 2 for hunk header (...)
			}
			return count
		}
	}
	return 0
}

// selectItem handles selection of current item
func (m *Model) selectItem() tea.Cmd {
	flatTree := m.flattenTree()
	if m.selectedIndex >= len(flatTree) {
		return nil
	}

	node := flatTree[m.selectedIndex]
	if node.isDir {
		// Toggle directory expansion
		m.toggleDirectory(node.path)
		return nil
	} else {
		// Reset diff scroll when selecting a new file
		m.diffScroll = 0
		// Load diff for the file
		return m.LoadDiff(node.path)
	}
}

// toggleDirectory toggles directory expansion
func (m *Model) toggleDirectory(path string) {
	var toggle func(nodes []TreeNode) bool
	toggle = func(nodes []TreeNode) bool {
		for i := range nodes {
			if nodes[i].path == path && nodes[i].isDir {
				nodes[i].isExpanded = !nodes[i].isExpanded
				return true
			}
			if nodes[i].isDir && toggle(nodes[i].children) {
				return true
			}
		}
		return false
	}
	toggle(m.fileTree)
}

// flattenTree flattens the tree for navigation
func (m *Model) flattenTree() []TreeNode {
	return flattenTree(m.fileTree, 0)
}

func flattenTree(nodes []TreeNode, depth int) []TreeNode {
	var result []TreeNode
	for _, node := range nodes {
		node.depth = depth
		result = append(result, node)
		if node.isDir && node.isExpanded {
			result = append(result, flattenTree(node.children, depth+1)...)
		}
	}
	return result
}

// buildFileTree builds the file tree from the list of changed files
func (m *Model) buildFileTree() {
	// Clear previous tree
	m.fileTree = nil

	root := &dirNode{
		subdirs: make(map[string]*dirNode),
	}

	// Build directory structure
	for _, file := range m.files {
		parts := splitPath(file.Path)
		current := root

		for i, part := range parts {
			if i == len(parts)-1 {
				// This is the file
				current.files = append(current.files, file)
			} else {
				// This is a directory
				if current.subdirs[part] == nil {
					current.subdirs[part] = &dirNode{
						path:    joinPath(parts[:i+1]),
						name:    part,
						subdirs: make(map[string]*dirNode),
					}
				}
				current = current.subdirs[part]
			}
		}
	}

	// Convert to TreeNode structure
	m.fileTree = buildTreeNodes(root, 0)

	// Auto-expand if there are directories
	if len(m.fileTree) == 1 && m.fileTree[0].isDir {
		m.fileTree[0].isExpanded = true
	}
}

func buildTreeNodes(dir *dirNode, depth int) []TreeNode {
	var nodes []TreeNode

	// Add subdirectories first
	for _, subdir := range dir.subdirs {
		childNodes := buildTreeNodes(subdir, depth+1)
		totalAdded, totalRemoved := getDirStats(subdir)
		nodes = append(nodes, TreeNode{
			name:        subdir.name,
			path:        subdir.path,
			isDir:       true,
			isExpanded:  false,
			children:    childNodes,
			changeType:  getDirChangeType(subdir),
			linesAdded:  totalAdded,
			linesRemoved: totalRemoved,
		})
	}

	// Add files
	for _, file := range dir.files {
		nodes = append(nodes, TreeNode{
			name:         getFileName(file.Path),
			path:         file.Path,
			isDir:        false,
			changeType:   file.ChangeType,
			linesAdded:   file.LinesAdded,
			linesRemoved: file.LinesRemoved,
		})
	}

	return nodes
}

func getDirChangeType(dir *dirNode) ChangeType {
	// Determine directory change type based on contents
	hasAdded := false
	hasDeleted := false

	for _, file := range dir.files {
		if file.ChangeType == Added {
			hasAdded = true
		} else if file.ChangeType == Deleted {
			hasDeleted = true
		}
	}

	for _, subdir := range dir.subdirs {
		ct := getDirChangeType(subdir)
		if ct == Added {
			hasAdded = true
		} else if ct == Deleted {
			hasDeleted = true
		}
	}

	if hasAdded && !hasDeleted {
		return Added
	} else if hasDeleted && !hasAdded {
		return Deleted
	}
	return Modified
}

func getDirStats(dir *dirNode) (added int, removed int) {
	// Calculate total stats for directory
	for _, file := range dir.files {
		added += file.LinesAdded
		removed += file.LinesRemoved
	}
	for _, subdir := range dir.subdirs {
		subAdded, subRemoved := getDirStats(subdir)
		added += subAdded
		removed += subRemoved
	}
	return added, removed
}

func splitPath(path string) []string {
	parts := []string{}
	current := ""
	for _, ch := range path {
		if ch == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func joinPath(parts []string) string {
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "/"
		}
		result += part
	}
	return result
}

func getFileName(path string) string {
	parts := splitPath(path)
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

// checkForChanges checks if the git repo has changed and reloads if necessary
func (m Model) checkForChanges() tea.Cmd {
	return func() tea.Msg {
		// Get current files and compute a hash
		files, err := GetChangedFiles(m.diffMode)
		if err != nil {
			return errMsg{err}
		}

		// Compute a simple hash of the current state
		currentHash := computeFileHash(files)

		if currentHash != m.lastFileHash {
			// Files have changed, reload everything
			return filesChangedMsg{
				files: files,
				hash:  currentHash,
			}
		}

		// No changes, just continue ticking
		return TickMsg{time: 0}
	}
}

// computeFileHash creates a simple hash string from the list of files
func computeFileHash(files []FileDiff) string {
	if len(files) == 0 {
		return "empty"
	}

	result := ""
	for _, f := range files {
		result += f.Path + string(rune(f.ChangeType))
	}
	return result
}

// GetTotalStats returns total lines added and removed across all files
func (m Model) GetTotalStats() (files int, added int, removed int) {
	for _, f := range m.diffFiles {
		files++
		added += f.LinesAdded
		removed += f.LinesRemoved
	}
	return files, added, removed
}
