package main

import (
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
			m.moveUp()
			return m, nil

		case "down", "j":
			m.moveDown()
			return m, nil

		case "tab":
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
			m.diffFiles = nil
			return m, m.LoadFiles()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case gitInfoMsg:
		m.rootPath = msg.rootPath
		m.branch = msg.branch
		return m, nil

	case filesLoadedMsg:
		m.files = msg.files
		m.buildFileTree()
		return m, nil

	case diffLoadedMsg:
		m.diffFiles = append(m.diffFiles, msg.file)
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
		nodes = append(nodes, TreeNode{
			name:      subdir.name,
			path:      subdir.path,
			isDir:     true,
			isExpanded: false,
			children:  buildTreeNodes(subdir, depth+1),
			changeType: getDirChangeType(subdir),
		})
	}

	// Add files
	for _, file := range dir.files {
		nodes = append(nodes, TreeNode{
			name:       getFileName(file.Path),
			path:       file.Path,
			isDir:      false,
			changeType: file.ChangeType,
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
