package main

import (
	"fmt"
	"hash/fnv"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
)

// dirNode is used for building the file tree
type dirNode struct {
	path    string
	name    string
	files   []FileDiff
	subdirs map[string]*dirNode
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(typed)
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	default:
		return m.handleAsyncMsg(typed)
	}
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Any key other than "g" clears pending "gg" state.
	if key != "g" {
		m.vimPendingG = false
	}

	// When help is visible, only allow help toggle and quit keys.
	if m.showHelp && key != "?" && key != "q" && key != "ctrl+c" {
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		if m.watcher != nil {
			_ = m.watcher.Close()
		}
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if m.panel == DiffPanel {
			if key == "k" && m.diffViewMode == DiffOnly {
				m.jumpToPrevHunk()
			} else {
				m.moveDiffUp()
			}
		} else {
			m.moveUp()
		}
		return m, nil
	case "down", "j":
		if m.panel == DiffPanel {
			if key == "j" && m.diffViewMode == DiffOnly {
				m.jumpToNextHunk()
			} else {
				m.moveDiffDown()
			}
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
	case "g":
		// Vim-style: "gg" jumps to top in whole-file/diff panel navigation.
		if m.diffViewMode == WholeFile || m.panel == DiffPanel {
			if m.vimPendingG {
				m.moveDiffToTop()
				m.vimPendingG = false
			} else {
				m.vimPendingG = true
			}
		}
		return m, nil
	case "G":
		// Vim-style: "G" jumps to bottom in whole-file/diff panel navigation.
		if m.diffViewMode == WholeFile || m.panel == DiffPanel {
			m.moveDiffToBottom()
		}
		return m, nil
	case "tab":
		// Don't allow switching to file tree panel in whole file mode.
		if m.diffViewMode != WholeFile {
			if m.panel == FileTreePanel {
				m.panel = DiffPanel
			} else {
				m.panel = FileTreePanel
			}
		}
		return m, nil
	case "enter", " ":
		if m.panel != FileTreePanel {
			return m, nil
		}
		return m, m.selectItem()
	case "s":
		return m, m.toggleDiffMode()
	case "f":
		return m, m.toggleDiffViewMode()
	case "o":
		// Expand surrounding context in diff-only mode.
		if m.diffViewMode == DiffOnly {
			m.diffContext += DefaultDiffContext
			return m, m.reloadCurrentDiffs()
		}
		return m, nil
	case "O":
		// Reset context expansion in diff-only mode.
		if m.diffViewMode == DiffOnly {
			m.diffContext = DefaultDiffContext
			return m, m.reloadCurrentDiffs()
		}
		return m, nil
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) toggleDiffMode() tea.Cmd {
	switch m.diffMode {
	case Unstaged:
		m.diffMode = Staged
	case Staged:
		m.diffMode = BranchCompare
	default:
		m.diffMode = Unstaged
	}

	m.selectedIndex = 0
	m.scrollOffset = 0
	m.diffScroll = 0
	m.diffFiles = nil
	m.files = nil
	m.commits = nil
	m.selectedCommit = nil

	if m.diffMode == BranchCompare {
		return tea.Batch(m.LoadCommitsAhead(), m.LoadBranchCompareDiff(nil))
	}
	return tea.Batch(m.LoadFiles(), m.LoadAllDiffs())
}

func (m *Model) toggleDiffViewMode() tea.Cmd {
	if m.diffViewMode == DiffOnly {
		m.diffViewMode = WholeFile
		m.panel = DiffPanel
	} else {
		m.diffViewMode = DiffOnly
	}

	m.diffScroll = 0
	m.diffFiles = nil

	if m.diffMode == BranchCompare {
		return tea.Batch(m.LoadCommitsAhead(), m.LoadBranchCompareDiff(m.commits))
	}
	return m.LoadAllDiffs()
}

func (m Model) handleAsyncMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case gitInfoMsg:
		m.rootPath = typed.rootPath
		m.branch = typed.branch
		watcher, err := NewWatcher(m.rootPath)
		if err != nil {
			if m.logger != nil {
				m.logger.Warn("Failed to create file watcher", map[string]interface{}{"error": err})
			}
			return m, nil
		}
		m.watcher = watcher
		return m, watcher.WaitForChange()
	case FSChangeMsg:
		cmd := m.checkForChanges()
		if m.watcher != nil {
			return m, tea.Batch(cmd, m.watcher.WaitForChange())
		}
		return m, cmd
	case filesLoadedMsg:
		m.files = typed.files
		m.err = nil
		if m.diffMode != BranchCompare && len(m.diffFiles) > 0 {
			m.files = mergeFilesWithDiffStats(m.files, m.diffFiles)
		}
		m.lastFileHash = computeFilesAndDiffHash(m.files, m.diffFiles)
		m.buildFileTree()
		return m, nil
	case allDiffsLoadedMsg:
		m.diffFiles = typed.files
		m.err = nil
		if m.diffMode == BranchCompare {
			m.lastFileHash = computeBranchCompareHash(typed.files, m.commits)
			m.files = aggregateBranchCompareFiles(typed.files)
		} else {
			m.files = mergeFilesWithDiffStats(m.files, typed.files)
			m.lastFileHash = computeFilesAndDiffHash(m.files, typed.files)
		}
		m.buildFileTree()
		if m.selectedIndex >= len(m.flattenTree()) {
			m.selectedIndex = 0
		}
		return m, nil
	case commitsLoadedMsg:
		m.commits = typed.commits
		m.err = nil
		m.selectedCommit = nil
		return m, nil
	case filesChangedMsg:
		m.lastFileHash = typed.hash
		if m.diffMode != BranchCompare {
			m.files = typed.files
			m.buildFileTree()
		}
		m.diffFiles = nil
		if m.diffMode == BranchCompare {
			return m, tea.Batch(m.LoadCommitsAhead(), m.LoadBranchCompareDiff(m.commits))
		}
		return m, m.LoadAllDiffs()
	case diffLoadedMsg:
		if typed.file.Path == "" {
			return m, nil
		}
		for i := range m.diffFiles {
			if m.diffFiles[i].Path == typed.file.Path {
				m.diffFiles[i] = typed.file
				return m, nil
			}
		}
		m.diffFiles = append(m.diffFiles, typed.file)
		return m, nil
	case ShowHelpMsg:
		m.showHelp = true
		return m, nil
	case HideHelpMsg:
		m.showHelp = false
		return m, nil
	case errMsg:
		m.err = typed.err
		return m, nil
	case clearErrorMsg:
		m.err = nil
		return m, nil
	default:
		return m, nil
	}
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
	maxIndex := len(m.flattenTree()) - 1

	if m.selectedIndex < maxIndex {
		m.selectedIndex++

		// Auto scroll if needed
		visibleHeight := m.visibleContentRows()
		if m.selectedIndex >= m.scrollOffset+visibleHeight {
			m.scrollOffset = m.selectedIndex - visibleHeight + 1
		}
	}
}

// movePageUp moves the selection up by a page
func (m *Model) movePageUp() {
	visibleHeight := m.visibleContentRows()

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
	visibleHeight := m.visibleContentRows()

	maxIndex := len(m.flattenTree()) - 1
	if maxIndex < 0 {
		m.selectedIndex = 0
		m.scrollOffset = 0
		return
	}

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
	visibleHeight := m.visibleContentRows()

	if m.diffScroll < totalLines-visibleHeight {
		m.diffScroll++
	}
}

// moveDiffPageUp scrolls the diff view up by a page
func (m *Model) moveDiffPageUp() {
	visibleHeight := m.visibleContentRows()

	m.diffScroll -= visibleHeight
	if m.diffScroll < 0 {
		m.diffScroll = 0
	}
}

// moveDiffPageDown scrolls the diff view down by a page
func (m *Model) moveDiffPageDown() {
	visibleHeight := m.visibleContentRows()

	totalLines := m.getDiffLineCount()
	m.diffScroll += visibleHeight
	if m.diffScroll > totalLines-visibleHeight {
		m.diffScroll = max(0, totalLines-visibleHeight)
	}
}

func (m Model) reloadCurrentDiffs() tea.Cmd {
	if m.diffMode == BranchCompare {
		return tea.Batch(m.LoadCommitsAhead(), m.LoadBranchCompareDiff(m.commits))
	}
	return m.LoadAllDiffs()
}

// moveDiffToTop scrolls the diff view to the top.
func (m *Model) moveDiffToTop() {
	m.diffScroll = 0
}

// moveDiffToBottom scrolls the diff view to the bottom.
func (m *Model) moveDiffToBottom() {
	totalLines := m.getDiffLineCount()
	visibleHeight := m.visibleContentRows()
	m.diffScroll = max(0, totalLines-visibleHeight)
}

func (m Model) visibleContentRows() int {
	// Terminal rows minus header/footer rows and panel border rows.
	rows := m.height - 5
	if rows < 1 {
		rows = 1
	}
	return rows
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// getDiffLineCount returns the total number of lines in the current diff
func (m *Model) getDiffLineCount() int {
	filesToRender := m.getSelectedDiffFiles()
	if len(filesToRender) == 0 {
		if m.diffMode == BranchCompare {
			return 3 // branch header + blank + message
		}
		return 1 // message
	}

	lineCount := 0
	if m.diffMode == BranchCompare {
		lineCount += 2 // branch header + blank
	}

	for fileIdx, selectedFile := range filesToRender {
		if fileIdx > 0 {
			lineCount += 2 // blank + separator
		}

		lineCount++ // file header
		if len(selectedFile.Hunks) == 0 {
			lineCount++ // empty-file message
			continue
		}

		for _, hunk := range selectedFile.Hunks {
			lineCount++ // hunk separator
			lineCount += len(hunk.Lines)
		}
	}

	return lineCount
}

// selectItem handles selection of current item
func (m *Model) selectItem() tea.Cmd {
	flatTree := m.flattenTree()
	if m.selectedIndex < 0 || m.selectedIndex >= len(flatTree) {
		return nil
	}

	node := flatTree[m.selectedIndex]
	if node.isDir {
		// Toggle directory expansion
		m.toggleDirectory(node.path)
		return nil
	} else {
		// In branch compare mode, file diffs are already loaded.
		if m.diffMode == BranchCompare {
			m.diffScroll = 0
			return nil
		}

		// Reset diff scroll when selecting a new file
		m.diffScroll = 0
		// Load diff for the file
		return m.LoadDiff(node.path)
	}
}

func (m *Model) jumpToNextHunk() {
	hunkStarts := m.getCurrentHunkStartLines()
	if len(hunkStarts) == 0 {
		return
	}

	for _, start := range hunkStarts {
		if start > m.diffScroll {
			m.diffScroll = start
			return
		}
	}

	m.diffScroll = hunkStarts[len(hunkStarts)-1]
}

func (m *Model) jumpToPrevHunk() {
	hunkStarts := m.getCurrentHunkStartLines()
	if len(hunkStarts) == 0 {
		return
	}

	for i := len(hunkStarts) - 1; i >= 0; i-- {
		if hunkStarts[i] < m.diffScroll {
			m.diffScroll = hunkStarts[i]
			return
		}
	}

	m.diffScroll = hunkStarts[0]
}

func (m Model) getCurrentHunkStartLines() []int {
	filesToRender := m.getSelectedDiffFiles()
	if len(filesToRender) == 0 {
		return nil
	}

	lineNum := 0
	if m.diffMode == BranchCompare {
		lineNum += 2 // branch header + blank
	}

	var starts []int
	for fileIdx, selectedFile := range filesToRender {
		if fileIdx > 0 {
			lineNum += 2 // blank + separator
		}

		lineNum++ // file header
		if len(selectedFile.Hunks) == 0 {
			lineNum++ // no-hunk message
			continue
		}

		for _, hunk := range selectedFile.Hunks {
			starts = append(starts, lineNum)
			lineNum++ // hunk separator line
			lineNum += len(hunk.Lines)
		}
	}

	return starts
}

func (m Model) getSelectedDiffFiles() []*FileDiff {
	flatTree := m.flattenTree()
	if m.selectedIndex < 0 || m.selectedIndex >= len(flatTree) {
		return nil
	}

	node := flatTree[m.selectedIndex]
	if node.isDir {
		return nil
	}

	var filesToRender []*FileDiff
	if m.diffMode == BranchCompare {
		for i := range m.diffFiles {
			if m.diffFiles[i].Path == node.path {
				filesToRender = append(filesToRender, &m.diffFiles[i])
			}
		}
		return filesToRender
	}

	for i := range m.diffFiles {
		if m.diffFiles[i].Path == node.path {
			filesToRender = append(filesToRender, &m.diffFiles[i])
			break
		}
	}
	return filesToRender
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
	subdirNames := make([]string, 0, len(dir.subdirs))
	for name := range dir.subdirs {
		subdirNames = append(subdirNames, name)
	}
	sort.Strings(subdirNames)
	for _, name := range subdirNames {
		subdir := dir.subdirs[name]
		childNodes := buildTreeNodes(subdir, depth+1)
		totalAdded, totalRemoved := getDirStats(subdir)
		nodes = append(nodes, TreeNode{
			name:         subdir.name,
			path:         subdir.path,
			isDir:        true,
			isExpanded:   true,
			children:     childNodes,
			changeType:   getDirChangeType(subdir),
			linesAdded:   totalAdded,
			linesRemoved: totalRemoved,
		})
	}

	// Add files
	sort.Slice(dir.files, func(i, j int) bool {
		return dir.files[i].Path < dir.files[j].Path
	})
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
		if m.git == nil {
			// No git service, can't check
			return nil
		}

		if m.diffMode == BranchCompare {
			commits, err := m.git.GetCommitsAheadOfMain()
			if err != nil {
				if m.logger != nil {
					m.logger.Error("Failed to check commits in branch compare", err, nil)
				}
				return nil
			}

			unifiedDiffs, err := m.git.GetUnifiedBranchCompareDiff(m.diffViewMode, m.diffContext, m.logger)
			if err != nil {
				if m.logger != nil {
					m.logger.Error("Failed to check unified branch compare diff", err, nil)
				}
				return nil
			}

			currentHash := computeBranchCompareHash(unifiedDiffs, commits)

			if currentHash != m.lastFileHash {
				if m.logger != nil {
					m.logger.Info("Repository changes detected", map[string]interface{}{
						"previous_hash": m.lastFileHash,
						"new_hash":      currentHash,
					})
				}
				return filesChangedMsg{hash: currentHash}
			}
			return nil
		}

		files, err := m.git.GetChangedFiles(m.diffMode)
		if err != nil {
			if m.logger != nil {
				m.logger.Error("Failed to check file list for changes", err, map[string]interface{}{
					"mode": m.diffMode,
				})
			}
			return nil
		}

		diffs, err := m.git.GetDiffWithContext(m.diffMode, m.diffViewMode, m.diffContext, m.logger)
		if err != nil {
			if m.logger != nil {
				m.logger.Error("Failed to check diff content for changes", err, map[string]interface{}{
					"mode": m.diffMode,
				})
			}
			return nil
		}
		currentHash := computeFilesAndDiffHash(files, diffs)

		if currentHash != m.lastFileHash {
			if m.logger != nil {
				m.logger.Info("Repository changes detected", map[string]interface{}{
					"previous_hash": m.lastFileHash,
					"new_hash":      currentHash,
					"file_count":    len(files),
				})
			}
			return filesChangedMsg{files: files, hash: currentHash}
		}

		// No changes
		return nil
	}
}

// computeFileHash creates a simple hash string from the list of files
func computeFileHash(files []FileDiff) string {
	if len(files) == 0 {
		return "empty"
	}

	sorted := append([]FileDiff(nil), files...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Path == sorted[j].Path {
			return sorted[i].ChangeType < sorted[j].ChangeType
		}
		return sorted[i].Path < sorted[j].Path
	})

	h := fnv.New64a()
	for _, f := range sorted {
		_, _ = h.Write([]byte(fmt.Sprintf("%s|%d|%d|%d\n", f.Path, f.ChangeType, f.LinesAdded, f.LinesRemoved)))
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func computeDiffHash(files []FileDiff) string {
	if len(files) == 0 {
		return "empty"
	}

	sorted := append([]FileDiff(nil), files...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Path == sorted[j].Path {
			return sorted[i].ChangeType < sorted[j].ChangeType
		}
		return sorted[i].Path < sorted[j].Path
	})

	h := fnv.New64a()
	for _, f := range sorted {
		_, _ = h.Write([]byte(fmt.Sprintf("%s|%d|%d|%d\n", f.Path, f.ChangeType, f.LinesAdded, f.LinesRemoved)))
		for _, hk := range f.Hunks {
			_, _ = h.Write([]byte(fmt.Sprintf("@@%d,%d,%d,%d\n", hk.OldStart, hk.OldCount, hk.NewStart, hk.NewCount)))
			for _, dl := range hk.Lines {
				_, _ = h.Write([]byte(fmt.Sprintf("%d|%s\n", dl.Type, dl.Content)))
			}
		}
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func computeBranchCompareHash(files []FileDiff, commits []Commit) string {
	base := computeDiffHash(files)
	if len(commits) == 0 {
		return base
	}

	h := fnv.New64a()
	_, _ = h.Write([]byte(base))
	for _, c := range commits {
		_, _ = h.Write([]byte("|" + c.Hash))
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func computeFilesAndDiffHash(files []FileDiff, diffs []FileDiff) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte("files=" + computeFileHash(files)))
	_, _ = h.Write([]byte("|diffs=" + computeDiffHash(diffs)))
	return fmt.Sprintf("%x", h.Sum64())
}

func mergeFilesWithDiffStats(files []FileDiff, diffs []FileDiff) []FileDiff {
	if len(diffs) == 0 {
		return files
	}

	statsByPath := make(map[string]FileDiff, len(diffs))
	for _, d := range diffs {
		statsByPath[d.Path] = d
	}

	if len(files) == 0 {
		merged := make([]FileDiff, len(diffs))
		copy(merged, diffs)
		return merged
	}

	merged := make([]FileDiff, 0, len(files))
	for _, f := range files {
		if d, ok := statsByPath[f.Path]; ok {
			f.LinesAdded = d.LinesAdded
			f.LinesRemoved = d.LinesRemoved
			f.ChangeType = d.ChangeType
		}
		merged = append(merged, f)
	}
	return merged
}

func aggregateBranchCompareFiles(diffFiles []FileDiff) []FileDiff {
	byPath := make(map[string]FileDiff)
	order := make([]string, 0, len(diffFiles))

	for _, f := range diffFiles {
		existing, ok := byPath[f.Path]
		if !ok {
			byPath[f.Path] = FileDiff{
				Path:         f.Path,
				ChangeType:   f.ChangeType,
				LinesAdded:   f.LinesAdded,
				LinesRemoved: f.LinesRemoved,
			}
			order = append(order, f.Path)
			continue
		}

		existing.LinesAdded += f.LinesAdded
		existing.LinesRemoved += f.LinesRemoved
		existing.ChangeType = Modified
		byPath[f.Path] = existing
	}

	result := make([]FileDiff, 0, len(order))
	for _, path := range order {
		result = append(result, byPath[path])
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
