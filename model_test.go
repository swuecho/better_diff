package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModel(t *testing.T) {
	model := NewModel()

	if model.panel != FileTreePanel {
		t.Errorf("NewModel() panel = %v, want %v", model.panel, FileTreePanel)
	}

	if model.diffMode != Unstaged {
		t.Errorf("NewModel() diffMode = %v, want %v", model.diffMode, Unstaged)
	}

	if model.diffViewMode != DiffOnly {
		t.Errorf("NewModel() diffViewMode = %v, want %v", model.diffViewMode, DiffOnly)
	}

	if model.scrollOffset != 0 {
		t.Errorf("NewModel() scrollOffset = %v, want 0", model.scrollOffset)
	}

	if model.diffScroll != 0 {
		t.Errorf("NewModel() diffScroll = %v, want 0", model.diffScroll)
	}
}

func TestModelInit(t *testing.T) {
	model := NewModel()

	// Init should return commands
	cmd := model.Init()
	if cmd == nil {
		t.Error("Model.Init() returned nil command")
	}
}

func TestModelUpdateQuit(t *testing.T) {
	model := NewModel()

	// Test 'q' key
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	newModel, cmd := model.Update(msg)

	if cmd == nil {
		t.Error("Update('q') should return a quit command")
	}

	if !newModel.(Model).quitting {
		t.Error("Update('q') should set quitting to true")
	}
}

func TestModelUpdateCtrlC(t *testing.T) {
	model := NewModel()

	// Test ctrl+c
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	newModel, cmd := model.Update(msg)

	if cmd == nil {
		t.Error("Update(ctrl+c) should return a quit command")
	}

	if !newModel.(Model).quitting {
		t.Error("Update(ctrl+c) should set quitting to true")
	}
}

func TestModelUpdateNavigation(t *testing.T) {
	model := NewModel()
	// Add some files to test navigation
	model.files = []FileDiff{
		{Path: "file1.txt"},
		{Path: "file2.txt"},
		{Path: "file3.txt"},
	}
	model.buildFileTree()

	// Test down arrow
	msg := tea.KeyMsg{Type: tea.KeyDown}
	newModel, _ := model.Update(msg)

	if newModel.(Model).selectedIndex != 1 {
		t.Errorf("Update(down) selectedIndex = %v, want 1", newModel.(Model).selectedIndex)
	}

	// Test up arrow
	msg = tea.KeyMsg{Type: tea.KeyUp}
	newModel, _ = model.Update(msg)

	if newModel.(Model).selectedIndex != 0 {
		t.Errorf("Update(up) selectedIndex = %v, want 0", newModel.(Model).selectedIndex)
	}
}

func TestModelUpdateToggleStaged(t *testing.T) {
	model := NewModel()

	// Test 's' key to toggle staging
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	newModel, _ := model.Update(msg)

	if newModel.(Model).diffMode != Staged {
		t.Errorf("Update('s') diffMode = %v, want %v", newModel.(Model).diffMode, Staged)
	}

	// Toggle again
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	newModel, _ = newModel.Update(msg)

	if newModel.(Model).diffMode != Unstaged {
		t.Errorf("Update('s') second toggle diffMode = %v, want %v", newModel.(Model).diffMode, Unstaged)
	}
}

func TestModelUpdateToggleViewMode(t *testing.T) {
	model := NewModel()

	// Test 'f' key to toggle view mode
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}
	newModel, _ := model.Update(msg)

	if newModel.(Model).diffViewMode != WholeFile {
		t.Errorf("Update('f') diffViewMode = %v, want %v", newModel.(Model).diffViewMode, WholeFile)
	}

	// Toggle again
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}
	newModel, _ = newModel.Update(msg)

	if newModel.(Model).diffViewMode != DiffOnly {
		t.Errorf("Update('f') second toggle diffViewMode = %v, want %v", newModel.(Model).diffViewMode, DiffOnly)
	}
}

func TestModelUpdateTogglePanel(t *testing.T) {
	model := NewModel()

	// Test tab key
	msg := tea.KeyMsg{Type: tea.KeyTab}
	newModel, _ := model.Update(msg)

	if newModel.(Model).panel != DiffPanel {
		t.Errorf("Update(tab) panel = %v, want %v", newModel.(Model).panel, DiffPanel)
	}

	// Toggle again
	msg = tea.KeyMsg{Type: tea.KeyTab}
	newModel, _ = newModel.Update(msg)

	if newModel.(Model).panel != FileTreePanel {
		t.Errorf("Update(tab) second toggle panel = %v, want %v", newModel.(Model).panel, FileTreePanel)
	}
}

func TestModelUpdateTabInWholeFileMode(t *testing.T) {
	model := NewModel()
	model.diffViewMode = WholeFile

	// In whole file mode, tab should not switch panels
	msg := tea.KeyMsg{Type: tea.KeyTab}
	newModel, _ := model.Update(msg)

	if newModel.(Model).panel != FileTreePanel {
		t.Errorf("Update(tab) in WholeFileMode should not change panel, got %v", newModel.(Model).panel)
	}
}

func TestModelWindowSize(t *testing.T) {
	model := NewModel()

	msg := tea.WindowSizeMsg{
		Width:  80,
		Height: 24,
	}

	newModel, _ := model.Update(msg)

	m := newModel.(Model)
	if m.width != 80 {
		t.Errorf("Update(WindowSizeMsg) width = %v, want 80", m.width)
	}
	if m.height != 24 {
		t.Errorf("Update(WindowSizeMsg) height = %v, want 24", m.height)
	}
}

func TestGetTotalStats(t *testing.T) {
	model := NewModel()

	model.diffFiles = []FileDiff{
		{Path: "file1.txt", LinesAdded: 5, LinesRemoved: 2},
		{Path: "file2.txt", LinesAdded: 10, LinesRemoved: 3},
		{Path: "file3.txt", LinesAdded: 1, LinesRemoved: 0},
	}

	files, added, removed := model.GetTotalStats()

	if files != 3 {
		t.Errorf("GetTotalStats() files = %v, want 3", files)
	}
	if added != 16 {
		t.Errorf("GetTotalStats() added = %v, want 16", added)
	}
	if removed != 5 {
		t.Errorf("GetTotalStats() removed = %v, want 5", removed)
	}
}

func TestGetTotalStatsEmpty(t *testing.T) {
	model := NewModel()
	model.diffFiles = []FileDiff{}

	files, added, removed := model.GetTotalStats()

	if files != 0 {
		t.Errorf("GetTotalStats() empty files = %v, want 0", files)
	}
	if added != 0 {
		t.Errorf("GetTotalStats() empty added = %v, want 0", added)
	}
	if removed != 0 {
		t.Errorf("GetTotalStats() empty removed = %v, want 0", removed)
	}
}

func TestFilesLoadedMsg(t *testing.T) {
	model := NewModel()

	files := []FileDiff{
		{Path: "file1.txt", ChangeType: Modified},
		{Path: "file2.txt", ChangeType: Added},
	}

	msg := filesLoadedMsg{files: files}
	newModel, _ := model.Update(msg)

	m := newModel.(Model)
	if len(m.files) != 2 {
		t.Errorf("filesLoadedMsg files length = %v, want 2", len(m.files))
	}
	if m.lastFileHash == "" {
		t.Error("filesLoadedMsg should set lastFileHash")
	}
}

func TestAllDiffsLoadedMsg(t *testing.T) {
	model := NewModel()

	files := []FileDiff{
		{Path: "file1.txt", LinesAdded: 5},
		{Path: "file2.txt", LinesAdded: 3},
	}

	msg := allDiffsLoadedMsg{files: files}
	newModel, _ := model.Update(msg)

	m := newModel.(Model)
	if len(m.diffFiles) != 2 {
		t.Errorf("allDiffsLoadedMsg diffFiles length = %v, want 2", len(m.diffFiles))
	}
}

func TestDiffLoadedMsg(t *testing.T) {
	model := NewModel()
	model.diffFiles = []FileDiff{}

	file := FileDiff{Path: "file1.txt", LinesAdded: 5}
	msg := diffLoadedMsg{file: file}
	newModel, _ := model.Update(msg)

	m := newModel.(Model)
	if len(m.diffFiles) != 1 {
		t.Errorf("diffLoadedMsg diffFiles length = %v, want 1", len(m.diffFiles))
	}
	if m.diffFiles[0].Path != "file1.txt" {
		t.Errorf("diffLoadedMsg file path = %v, want file1.txt", m.diffFiles[0].Path)
	}
}

func TestDiffLoadedMsgReplaceExisting(t *testing.T) {
	model := NewModel()
	model.diffFiles = []FileDiff{
		{Path: "file1.txt", LinesAdded: 2},
	}

	file := FileDiff{Path: "file1.txt", LinesAdded: 5}
	msg := diffLoadedMsg{file: file}
	newModel, _ := model.Update(msg)

	m := newModel.(Model)
	if len(m.diffFiles) != 1 {
		t.Errorf("diffLoadedMsg diffFiles length = %v, want 1 (replace, not append)", len(m.diffFiles))
	}
	if m.diffFiles[0].LinesAdded != 5 {
		t.Errorf("diffLoadedMsg LinesAdded = %v, want 5 (replaced)", m.diffFiles[0].LinesAdded)
	}
}

func TestDiffLoadedMsgSkipEmpty(t *testing.T) {
	model := NewModel()
	initialLen := len(model.diffFiles)

	file := FileDiff{} // Empty file
	msg := diffLoadedMsg{file: file}
	newModel, _ := model.Update(msg)

	m := newModel.(Model)
	if len(m.diffFiles) != initialLen {
		t.Errorf("diffLoadedMsg should skip empty files, length = %v, want %v", len(m.diffFiles), initialLen)
	}
}

func TestGitInfoMsg(t *testing.T) {
	model := NewModel()

	msg := gitInfoMsg{
		rootPath: "/test/path",
		branch:   "main",
	}
	newModel, _ := model.Update(msg)

	m := newModel.(Model)
	if m.rootPath != "/test/path" {
		t.Errorf("gitInfoMsg rootPath = %v, want /test/path", m.rootPath)
	}
	if m.branch != "main" {
		t.Errorf("gitInfoMsg branch = %v, want main", m.branch)
	}
}

func TestErrMsg(t *testing.T) {
	model := NewModel()

	testErr := &testError{msg: "test error"}
	msg := errMsg{err: testErr}
	newModel, _ := model.Update(msg)

	m := newModel.(Model)
	if m.err == nil {
		t.Error("errMsg should set error")
	}
	if m.err.Error() != "test error" {
		t.Errorf("errMsg error = %v, want 'test error'", m.err)
	}
}

func TestClearErrorMsg(t *testing.T) {
	model := NewModel()
	model.err = &testError{msg: "test error"}

	msg := clearErrorMsg{}
	newModel, _ := model.Update(msg)

	m := newModel.(Model)
	if m.err != nil {
		t.Errorf("clearErrorMsg should clear error, got %v", m.err)
	}
}

func TestBuildFileTree(t *testing.T) {
	model := NewModel()

	model.files = []FileDiff{
		{Path: "src/main.go", ChangeType: Modified, LinesAdded: 5, LinesRemoved: 2},
		{Path: "src/utils.go", ChangeType: Added, LinesAdded: 10, LinesRemoved: 0},
		{Path: "README.md", ChangeType: Modified, LinesAdded: 1, LinesRemoved: 1},
	}

	model.buildFileTree()

	if len(model.fileTree) != 2 { // src/ and README.md
		t.Errorf("buildFileTree() tree length = %v, want 2", len(model.fileTree))
	}

	// First item should be src directory
	if !model.fileTree[0].isDir {
		t.Error("First item should be a directory")
	}

	// Second item should be README.md file
	if model.fileTree[1].isDir {
		t.Error("Second item should not be a directory")
	}
	if model.fileTree[1].path != "README.md" {
		t.Errorf("Second item path = %v, want README.md", model.fileTree[1].path)
	}
}

func TestFlattenTree(t *testing.T) {
	model := NewModel()

	model.files = []FileDiff{
		{Path: "src/main.go", ChangeType: Modified},
		{Path: "src/utils.go", ChangeType: Added},
		{Path: "README.md", ChangeType: Modified},
	}

	model.buildFileTree()
	model.fileTree[0].isExpanded = true // Expand src directory

	flat := model.flattenTree()

	// Should have: src/, src/main.go, src/utils.go, README.md
	if len(flat) != 4 {
		t.Errorf("flattenTree() length = %v, want 4", len(flat))
	}

	// Check depth
	if flat[0].depth != 0 {
		t.Errorf("flattenTree()[0] depth = %v, want 0", flat[0].depth)
	}
	if flat[1].depth != 1 {
		t.Errorf("flattenTree()[1] depth = %v, want 1", flat[1].depth)
	}
}

func TestToggleDirectory(t *testing.T) {
	model := NewModel()

	model.files = []FileDiff{
		{Path: "src/main.go", ChangeType: Modified},
	}

	model.buildFileTree()
	model.fileTree[0].isExpanded = false // Start collapsed

	// Toggle to expand
	model.toggleDirectory(model.fileTree[0].path)
	if !model.fileTree[0].isExpanded {
		t.Error("toggleDirectory() should expand directory")
	}

	// Toggle to collapse
	model.toggleDirectory(model.fileTree[0].path)
	if model.fileTree[0].isExpanded {
		t.Error("toggleDirectory() should collapse directory")
	}
}

func TestComputeFileHash(t *testing.T) {
	files1 := []FileDiff{
		{Path: "file1.txt", ChangeType: Modified},
		{Path: "file2.txt", ChangeType: Added},
	}

	files2 := []FileDiff{
		{Path: "file1.txt", ChangeType: Modified},
		{Path: "file2.txt", ChangeType: Added},
	}

	files3 := []FileDiff{
		{Path: "file1.txt", ChangeType: Modified},
		{Path: "file3.txt", ChangeType: Added},
	}

	hash1 := computeFileHash(files1)
	hash2 := computeFileHash(files2)
	hash3 := computeFileHash(files3)

	if hash1 != hash2 {
		t.Error("computeFileHash() should be same for same files")
	}

	if hash1 == hash3 {
		t.Error("computeFileHash() should be different for different files")
	}
}

func TestComputeFileHashEmpty(t *testing.T) {
	files := []FileDiff{}
	hash := computeFileHash(files)

	if hash != "empty" {
		t.Errorf("computeFileHash() empty = %v, want 'empty'", hash)
	}
}

// Helper types for testing

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// TestTickMsg tests the tick message processing
func TestTickMsg(t *testing.T) {
	model := NewModel()
	model.lastFileHash = "test"

	msg := TickMsg{time: 0}
	newModel, _ := model.Update(msg)

	// TickMsg should trigger checkForChanges
	// Since we haven't actually changed files, the hash should remain the same
	m := newModel.(Model)
	if m.lastFileHash != "test" {
		t.Errorf("TickMsg should not change hash if no changes, got %v", m.lastFileHash)
	}
}
