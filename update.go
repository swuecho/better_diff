package main

import (
	"fmt"
	"hash"
	"hash/fnv"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// dirNode is used for building the file tree
type dirNode struct {
	path    string
	name    string
	files   []FileDiff
	subdirs map[string]*dirNode
}

type diffLayout struct {
	totalLines int
	hunkStarts []int
}

type treeChangeSummary struct {
	linesAdded   int
	linesRemoved int
	hasAdded     bool
	hasDeleted   bool
}

func (s *treeChangeSummary) add(linesAdded, linesRemoved int, changeType ChangeType) {
	s.linesAdded += linesAdded
	s.linesRemoved += linesRemoved
	switch changeType {
	case Added:
		s.hasAdded = true
	case Deleted:
		s.hasDeleted = true
	}
}

func (s treeChangeSummary) changeType() ChangeType {
	if s.hasAdded && !s.hasDeleted {
		return Added
	}
	if s.hasDeleted && !s.hasAdded {
		return Deleted
	}
	return Modified
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

	// Handle search mode input
	if m.searchMode {
		return m.handleSearchInput(key, msg)
	}

	m.resetPendingVimTopJumpIfNeeded(key)
	if m.shouldIgnoreKey(key) {
		return m, nil
	}

	return m, m.runKeyAction(key)
}

// handleSearchInput handles keyboard input when in search mode
func (m *Model) handleSearchInput(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "ctrl+c":
		// Cancel search
		m.searchMode = false
		m.searchQuery = ""
		m.selectedIndex = 0
		m.scrollOffset = 0
		return m, nil
	case "enter":
		// Confirm search (exit mode but keep filter)
		m.searchMode = false
		m.selectedIndex = 0
		m.scrollOffset = 0
		return m, nil
	case "backspace":
		// Delete last character
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			m.selectedIndex = 0
			m.scrollOffset = 0
		}
		return m, nil
	default:
		// Add printable character to search query
		if len(msg.Runes) > 0 && isPrintableRune(msg.Runes[0]) {
			m.searchQuery += string(msg.Runes)
			m.selectedIndex = 0
			m.scrollOffset = 0
		}
		return m, nil
	}
}

// isPrintableRune checks if a rune is a printable character (not a control character)
func isPrintableRune(r rune) bool {
	return r >= 32 && r < 127
}

func (m *Model) runKeyAction(key string) tea.Cmd {
	switch key {
	case "q", "ctrl+c":
		return m.quitCmd()
	case "up", "k":
		m.handleUpKey(key)
	case "down", "j":
		m.handleDownKey(key)
	case "pgup":
		m.handlePageUp()
	case "pgdown":
		m.handlePageDown()
	case "g":
		m.handleVimTopJump()
	case "G":
		m.handleVimBottomJump()
	case "tab":
		m.togglePanel()
	case "enter", " ":
		return m.selectFileTreeItem()
	case "s":
		return m.toggleDiffMode()
	case "f":
		return m.toggleDiffViewMode()
	case "o":
		return m.adjustDiffContext(DefaultDiffContext)
	case "O":
		return m.resetDiffContext()
	case "?":
		m.toggleHelp()
	case "/":
		return m.enterSearchMode()
	}
	return nil
}

// enterSearchMode activates search mode for file tree filtering
func (m *Model) enterSearchMode() tea.Cmd {
	if m.panel != FileTreePanel || m.diffViewMode == WholeFile {
		return nil
	}
	m.searchMode = true
	return nil
}

func (m *Model) selectFileTreeItem() tea.Cmd {
	if m.panel != FileTreePanel {
		return nil
	}
	return m.selectItem()
}

func (m *Model) toggleHelp() {
	m.showHelp = !m.showHelp
}

func (m *Model) resetPendingVimTopJumpIfNeeded(key string) {
	if key != "g" {
		m.vimPendingG = false
	}
}

func (m Model) shouldIgnoreKey(key string) bool {
	return m.showHelp && key != "?" && key != "q" && key != "ctrl+c"
}

func (m *Model) quitCmd() tea.Cmd {
	if m.watcher != nil {
		if err := m.watcher.Close(); err != nil {
			m.logger.Warn("close file watcher", map[string]any{"error": err})
		}
	}
	m.quitting = true
	return tea.Quit
}

func (m *Model) handleUpKey(key string) {
	if m.panel != DiffPanel {
		m.moveUp()
		return
	}
	if key == "k" && m.diffViewMode == DiffOnly {
		m.jumpToPrevHunk()
		return
	}
	m.moveDiffUp()
}

func (m *Model) handleDownKey(key string) {
	if m.panel != DiffPanel {
		m.moveDown()
		return
	}
	if key == "j" && m.diffViewMode == DiffOnly {
		m.jumpToNextHunk()
		return
	}
	m.moveDiffDown()
}

func (m *Model) handlePageUp() {
	if m.panel == DiffPanel {
		m.moveDiffPageUp()
		return
	}
	m.movePageUp()
}

func (m *Model) handlePageDown() {
	if m.panel == DiffPanel {
		m.moveDiffPageDown()
		return
	}
	m.movePageDown()
}

func (m Model) canMoveDiffCursor() bool {
	return m.diffViewMode == WholeFile || m.panel == DiffPanel
}

func (m *Model) handleVimTopJump() {
	if !m.canMoveDiffCursor() {
		return
	}
	if m.vimPendingG {
		m.moveDiffToTop()
		m.vimPendingG = false
		return
	}
	m.vimPendingG = true
}

func (m *Model) handleVimBottomJump() {
	if m.canMoveDiffCursor() {
		m.moveDiffToBottom()
	}
}

func (m *Model) togglePanel() {
	if m.diffViewMode == WholeFile {
		return
	}
	if m.panel == FileTreePanel {
		m.panel = DiffPanel
		return
	}
	m.panel = FileTreePanel
}

func (m *Model) adjustDiffContext(delta int) tea.Cmd {
	if m.diffViewMode != DiffOnly {
		return nil
	}
	m.diffContext += delta
	return m.reloadCurrentDiffs()
}

func (m *Model) resetDiffContext() tea.Cmd {
	if m.diffViewMode != DiffOnly {
		return nil
	}
	m.diffContext = DefaultDiffContext
	return m.reloadCurrentDiffs()
}

func (m *Model) toggleDiffMode() tea.Cmd {
	m.diffMode = nextDiffMode(m.diffMode)
	m.resetSelectionAndLoadedData()
	// Clear search when changing modes
	m.searchQuery = ""
	m.searchMode = false
	return m.reloadByDiffMode()
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
	// Clear search when changing view modes
	m.searchQuery = ""
	m.searchMode = false

	return m.reloadDiffsForCurrentMode()
}

func (m Model) handleAsyncMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case gitInfoMsg:
		return m.handleGitInfoLoaded(typed)
	case FSChangeMsg:
		return m.handleFSChange()
	case filesLoadedMsg:
		m.applyFilesLoaded(typed)
	case allDiffsLoadedMsg:
		m.applyAllDiffsLoaded(typed)
	case commitsLoadedMsg:
		m.applyCommitsLoaded(typed)
	case filesChangedMsg:
		return m.handleFilesChanged(typed)
	case diffLoadedMsg:
		m.upsertLoadedDiff(typed.file)
	case ShowHelpMsg:
		m.setHelpVisibility(true)
	case HideHelpMsg:
		m.setHelpVisibility(false)
	case errMsg:
		m.err = typed.err
	case clearErrorMsg:
		m.err = nil
	default:
	}

	return m, nil
}

func (m *Model) setHelpVisibility(visible bool) {
	m.showHelp = visible
}

func (m Model) loadBranchCompareData() tea.Cmd {
	return tea.Batch(m.LoadCommitsAhead(), m.LoadBranchCompareDiff(m.commits))
}

func (m Model) handleGitInfoLoaded(msg gitInfoMsg) (tea.Model, tea.Cmd) {
	m.rootPath = msg.rootPath
	m.branch = msg.branch

	watcher, err := NewWatcher(m.rootPath)
	if err != nil {
		m.logger.Warn("create file watcher", map[string]any{"error": err})
		return m, nil
	}

	m.watcher = watcher
	return m, watcher.WaitForChange()
}

func (m Model) handleFSChange() (tea.Model, tea.Cmd) {
	checkCmd := m.checkForChanges()
	if m.watcher == nil {
		return m, checkCmd
	}
	return m, tea.Batch(checkCmd, m.watcher.WaitForChange())
}

func (m *Model) applyFilesLoaded(msg filesLoadedMsg) {
	m.files = msg.files
	m.err = nil

	if m.diffMode != BranchCompare && len(m.diffFiles) > 0 {
		m.files = mergeFilesWithDiffStats(m.files, m.diffFiles)
	}

	m.lastFileHash = computeFilesAndDiffHash(m.files, m.diffFiles)
	m.buildFileTree()
}

func (m *Model) applyAllDiffsLoaded(msg allDiffsLoadedMsg) {
	m.diffFiles = msg.files
	m.err = nil

	if m.diffMode == BranchCompare {
		m.lastFileHash = computeBranchCompareHash(msg.files, m.commits)
		m.files = aggregateBranchCompareFiles(msg.files)
	} else {
		m.files = mergeFilesWithDiffStats(m.files, msg.files)
		m.lastFileHash = computeFilesAndDiffHash(m.files, msg.files)
	}

	m.buildFileTree()
	if m.selectedIndex >= len(m.flattenTree()) {
		m.selectedIndex = 0
	}
}

func (m *Model) applyCommitsLoaded(msg commitsLoadedMsg) {
	m.commits = msg.commits
	m.err = nil
	m.selectedCommit = nil
}

func (m Model) handleFilesChanged(msg filesChangedMsg) (tea.Model, tea.Cmd) {
	m.lastFileHash = msg.hash
	if m.diffMode != BranchCompare {
		m.files = msg.files
		m.buildFileTree()
	}
	m.diffFiles = nil
	return m, m.reloadDiffsForCurrentMode()
}

func (m *Model) upsertLoadedDiff(file FileDiff) {
	if file.Path == "" {
		return
	}

	for i := range m.diffFiles {
		if m.diffFiles[i].Path == file.Path {
			m.diffFiles[i] = file
			return
		}
	}

	m.diffFiles = append(m.diffFiles, file)
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
	return m.reloadDiffsForCurrentMode()
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

// getDiffLineCount returns the total number of lines in the current diff
func (m *Model) getDiffLineCount() int {
	filesToRender := m.getSelectedDiffFiles()
	return m.computeDiffLayout(filesToRender).totalLines
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
	}

	// In branch compare mode, file diffs are already loaded.
	if m.diffMode == BranchCompare {
		m.diffScroll = 0
		return nil
	}

	// Reset diff scroll when selecting a new file.
	m.diffScroll = 0
	return m.LoadDiff(node.path)
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
	return m.computeDiffLayout(filesToRender).hunkStarts
}

func (m Model) computeDiffLayout(filesToRender []*FileDiff) diffLayout {
	layout := diffLayout{}
	if len(filesToRender) == 0 {
		if m.diffMode == BranchCompare {
			layout.totalLines = 3 // branch header + blank + message
		} else {
			layout.totalLines = 1 // message
		}
		return layout
	}

	lineNum := 0
	if m.diffMode == BranchCompare {
		lineNum += 2 // branch header + blank
	}

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
			layout.hunkStarts = append(layout.hunkStarts, lineNum)
			lineNum++ // hunk separator line
			lineNum += len(hunk.Lines)
		}
	}

	layout.totalLines = lineNum
	return layout
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

	matching := make([]*FileDiff, 0, 1)
	for i := range m.diffFiles {
		if m.diffFiles[i].Path != node.path {
			continue
		}
		matching = append(matching, &m.diffFiles[i])
		if m.diffMode != BranchCompare {
			break
		}
	}
	return matching
}

// toggleDirectory toggles directory expansion
func (m *Model) toggleDirectory(path string) {
	toggleDirectoryInNodes(m.fileTree, path)
}

// flattenTree flattens the tree for navigation
func (m *Model) flattenTree() []TreeNode {
	tree := m.fileTree
	if m.searchQuery != "" {
		tree = m.filterTree(m.fileTree, m.searchQuery)
	}
	result := make([]TreeNode, 0, len(tree))
	appendFlattenedTree(&result, tree, 0)
	return result
}

// filterTree filters the tree nodes based on search query
func (m *Model) filterTree(nodes []TreeNode, query string) []TreeNode {
	if query == "" {
		return nodes
	}

	query = strings.ToLower(query)
	var filtered []TreeNode

	for _, node := range nodes {
		if node.isDir {
			// For directories, check if any children match
			filteredChildren := m.filterTree(node.children, query)
			if len(filteredChildren) > 0 || strings.Contains(strings.ToLower(node.name), query) {
				filtered = append(filtered, TreeNode{
					name:         node.name,
					path:         node.path,
					isDir:        true,
					isExpanded:   true, // Always expand filtered results
					children:     filteredChildren,
					changeType:   node.changeType,
					linesAdded:   node.linesAdded,
					linesRemoved: node.linesRemoved,
				})
			}
		} else {
			// For files, check if name matches query
			if strings.Contains(strings.ToLower(node.name), query) || strings.Contains(strings.ToLower(node.path), query) {
				filtered = append(filtered, node)
			}
		}
	}

	return filtered
}

func toggleDirectoryInNodes(nodes []TreeNode, path string) bool {
	for i := range nodes {
		if nodes[i].path == path && nodes[i].isDir {
			nodes[i].isExpanded = !nodes[i].isExpanded
			return true
		}
		if nodes[i].isDir && toggleDirectoryInNodes(nodes[i].children, path) {
			return true
		}
	}
	return false
}

func appendFlattenedTree(dst *[]TreeNode, nodes []TreeNode, depth int) {
	for _, node := range nodes {
		node.depth = depth
		*dst = append(*dst, node)
		if node.isDir && node.isExpanded {
			appendFlattenedTree(dst, node.children, depth+1)
		}
	}
}

// buildFileTree builds the file tree from the list of changed files
func (m *Model) buildFileTree() {
	root := buildDirTree(m.files)
	m.fileTree = buildTreeNodes(root, 0)

	// Auto-expand if there are directories
	if len(m.fileTree) == 1 && m.fileTree[0].isDir {
		m.fileTree[0].isExpanded = true
	}
}

func buildDirTree(files []FileDiff) *dirNode {
	root := &dirNode{
		subdirs: make(map[string]*dirNode),
	}

	for _, file := range files {
		addFileToDirTree(root, file)
	}

	return root
}

func addFileToDirTree(root *dirNode, file FileDiff) {
	parts := splitPath(file.Path)
	if len(parts) == 0 {
		return
	}

	current := root
	for i, part := range parts {
		isFile := i == len(parts)-1
		if isFile {
			current.files = append(current.files, file)
			continue
		}
		current = ensureSubdir(current, parts, i, part)
	}
}

func ensureSubdir(current *dirNode, parts []string, idx int, name string) *dirNode {
	if child, exists := current.subdirs[name]; exists {
		return child
	}

	newNode := &dirNode{
		path:    joinPath(parts[:idx+1]),
		name:    name,
		subdirs: make(map[string]*dirNode),
	}
	current.subdirs[name] = newNode
	return newNode
}

func buildTreeNodes(dir *dirNode, depth int) []TreeNode {
	nodes, _, _, _ := buildTreeNodesWithSummary(dir, depth)
	return nodes
}

func buildTreeNodesWithSummary(dir *dirNode, depth int) ([]TreeNode, int, int, ChangeType) {
	var nodes []TreeNode
	summary := treeChangeSummary{}

	// Add subdirectories first
	subdirNames := make([]string, 0, len(dir.subdirs))
	for name := range dir.subdirs {
		subdirNames = append(subdirNames, name)
	}
	sort.Strings(subdirNames)
	for _, name := range subdirNames {
		subdir := dir.subdirs[name]
		childNodes, childAdded, childRemoved, childType := buildTreeNodesWithSummary(subdir, depth+1)
		summary.add(childAdded, childRemoved, childType)

		nodes = append(nodes, TreeNode{
			name:         subdir.name,
			path:         subdir.path,
			isDir:        true,
			isExpanded:   true,
			children:     childNodes,
			changeType:   childType,
			linesAdded:   childAdded,
			linesRemoved: childRemoved,
		})
	}

	// Add files
	sort.Slice(dir.files, func(i, j int) bool {
		return dir.files[i].Path < dir.files[j].Path
	})
	for _, file := range dir.files {
		summary.add(file.LinesAdded, file.LinesRemoved, file.ChangeType)

		nodes = append(nodes, TreeNode{
			name:         fileNameFromPath(file.Path),
			path:         file.Path,
			isDir:        false,
			changeType:   file.ChangeType,
			linesAdded:   file.LinesAdded,
			linesRemoved: file.LinesRemoved,
		})
	}

	return nodes, summary.linesAdded, summary.linesRemoved, summary.changeType()
}

func nextDiffMode(mode DiffMode) DiffMode {
	switch mode {
	case Unstaged:
		return Staged
	case Staged:
		return BranchCompare
	default:
		return Unstaged
	}
}

func (m *Model) resetSelectionAndLoadedData() {
	m.selectedIndex = 0
	m.scrollOffset = 0
	m.diffScroll = 0
	m.diffFiles = nil
	m.files = nil
	m.commits = nil
	m.selectedCommit = nil
}

func (m Model) reloadByDiffMode() tea.Cmd {
	if m.diffMode == BranchCompare {
		return tea.Batch(m.LoadCommitsAhead(), m.LoadBranchCompareDiff(nil))
	}
	return tea.Batch(m.LoadFiles(), m.LoadAllDiffs())
}

func (m Model) reloadDiffsForCurrentMode() tea.Cmd {
	if m.diffMode == BranchCompare {
		return m.loadBranchCompareData()
	}
	return m.LoadAllDiffs()
}

// checkForChanges checks if the git repo has changed and reloads if necessary
func (m Model) checkForChanges() tea.Cmd {
	return func() tea.Msg {
		if m.git == nil {
			// No git service, can't check
			return nil
		}

		if m.diffMode == BranchCompare {
			return m.checkBranchCompareChanges()
		}

		return m.checkWorkingTreeChanges()
	}
}

func (m Model) checkBranchCompareChanges() tea.Msg {
	commits, err := m.git.GetCommitsAheadOfMain()
	if err != nil {
		m.logger.Error("check commits in branch compare", err, nil)
		return nil
	}

	unifiedDiffs, err := m.git.GetUnifiedBranchCompareDiff(m.diffViewMode, m.diffContext, m.logger)
	if err != nil {
		m.logger.Error("check unified branch compare diff", err, nil)
		return nil
	}

	currentHash := computeBranchCompareHash(unifiedDiffs, commits)
	if currentHash == m.lastFileHash {
		return nil
	}

	m.logChangeDetected(currentHash, nil)
	return filesChangedMsg{hash: currentHash}
}

func (m Model) checkWorkingTreeChanges() tea.Msg {
	files, err := m.git.GetChangedFiles(m.diffMode)
	if err != nil {
		m.logger.Error("check file list for changes", err, map[string]any{
			"mode": m.diffMode,
		})
		return nil
	}

	diffs, err := m.git.GetDiffWithContext(m.diffMode, m.diffViewMode, m.diffContext, m.logger)
	if err != nil {
		m.logger.Error("check diff content for changes", err, map[string]any{
			"mode": m.diffMode,
		})
		return nil
	}

	currentHash := computeFilesAndDiffHash(files, diffs)
	if currentHash == m.lastFileHash {
		return nil
	}

	m.logChangeDetected(currentHash, map[string]any{"file_count": len(files)})
	return filesChangedMsg{files: files, hash: currentHash}
}

func (m Model) logChangeDetected(newHash string, extra map[string]any) {
	fields := map[string]any{
		"previous_hash": m.lastFileHash,
		"new_hash":      newHash,
	}
	for k, v := range extra {
		fields[k] = v
	}

	m.logger.Info("repository changes detected", fields)
}

// computeFileHash creates a simple hash string from the list of files
func computeFileHash(files []FileDiff) string {
	if len(files) == 0 {
		return "empty"
	}

	sorted := sortedFilesByPathAndType(files)
	h := fnv.New64a()
	for _, file := range sorted {
		writeFileDiffSummaryHash(h, file)
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func computeDiffHash(files []FileDiff) string {
	if len(files) == 0 {
		return "empty"
	}

	sorted := sortedFilesByPathAndType(files)
	h := fnv.New64a()
	for _, file := range sorted {
		writeFileDiffSummaryHash(h, file)
		for _, hunk := range file.Hunks {
			writeHashString(h, fmt.Sprintf("@@%d,%d,%d,%d\n", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount))
			for _, line := range hunk.Lines {
				writeHashString(h, fmt.Sprintf("%d|%s\n", line.Type, line.Content))
			}
		}
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func sortedFilesByPathAndType(files []FileDiff) []FileDiff {
	sorted := append([]FileDiff(nil), files...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Path == sorted[j].Path {
			return sorted[i].ChangeType < sorted[j].ChangeType
		}
		return sorted[i].Path < sorted[j].Path
	})
	return sorted
}

func writeFileDiffSummaryHash(hasher hash.Hash64, file FileDiff) {
	writeHashString(hasher, fmt.Sprintf("%s|%d|%d|%d\n", file.Path, file.ChangeType, file.LinesAdded, file.LinesRemoved))
}

func writeHashString(hasher hash.Hash64, content string) {
	// fnv hash writers used here do not return write errors.
	_, _ = hasher.Write([]byte(content))
}

func computeBranchCompareHash(files []FileDiff, commits []Commit) string {
	base := computeDiffHash(files)
	if len(commits) == 0 {
		return base
	}

	h := fnv.New64a()
	writeHashString(h, base)
	for _, c := range commits {
		writeHashString(h, "|"+c.Hash)
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func computeFilesAndDiffHash(files []FileDiff, diffs []FileDiff) string {
	h := fnv.New64a()
	writeHashString(h, "files="+computeFileHash(files))
	writeHashString(h, "|diffs="+computeDiffHash(diffs))
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
