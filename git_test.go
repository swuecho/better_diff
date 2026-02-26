package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// setupGitService creates a GitService for testing
func setupGitService(t *testing.T) *GitService {
	gitService, err := NewGitService()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository or git service init failed: %v", err)
	}
	return gitService
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "single line without newline",
			input:    "line1",
			expected: []string{"line1"},
		},
		{
			name:     "single line with trailing newline",
			input:    "line1\n",
			expected: []string{"line1"},
		},
		{
			name:     "multiple lines with trailing newline",
			input:    "line1\nline2\nline3\n",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "multiple lines without trailing newline",
			input:    "line1\nline2\nline3",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "only newlines",
			input:    "\n\n\n",
			expected: []string{"", "", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitLines(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("splitLines() length = %v, want %v", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("splitLines()[%d] = %v, want %v", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestComputeHunks(t *testing.T) {
	tests := []struct {
		name           string
		oldLines       []string
		newLines       []string
		expectedHunks  int
		expectedAdd    int
		expectedRemove int
	}{
		{
			name:           "no changes",
			oldLines:       []string{"line1", "line2", "line3"},
			newLines:       []string{"line1", "line2", "line3"},
			expectedHunks:  0,
			expectedAdd:    0,
			expectedRemove: 0,
		},
		{
			name:           "single line modification",
			oldLines:       []string{"line1", "line2", "line3"},
			newLines:       []string{"line1", "line2 modified", "line3"},
			expectedHunks:  1,
			expectedAdd:    1,
			expectedRemove: 1,
		},
		{
			name:           "add line at end",
			oldLines:       []string{"line1", "line2"},
			newLines:       []string{"line1", "line2", "line3"},
			expectedHunks:  1,
			expectedAdd:    1,
			expectedRemove: 0,
		},
		{
			name:           "delete line",
			oldLines:       []string{"line1", "line2", "line3"},
			newLines:       []string{"line1", "line3"},
			expectedHunks:  1,
			expectedAdd:    0,
			expectedRemove: 1,
		},
		{
			name:           "empty to content",
			oldLines:       []string{},
			newLines:       []string{"line1", "line2"},
			expectedHunks:  1,
			expectedAdd:    2,
			expectedRemove: 0,
		},
		{
			name:           "content to empty",
			oldLines:       []string{"line1", "line2"},
			newLines:       []string{},
			expectedHunks:  1,
			expectedAdd:    0,
			expectedRemove: 2,
		},
		{
			name:           "multiple changes",
			oldLines:       []string{"a", "b", "c", "d"},
			newLines:       []string{"a", "b modified", "c", "e"},
			expectedHunks:  1, // Diff algorithm merges nearby changes
			expectedAdd:    2,
			expectedRemove: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hunks, err := computeHunks(tt.oldLines, tt.newLines)
			if err != nil {
				t.Fatalf("computeHunks() error = %v", err)
			}

			if len(hunks) != tt.expectedHunks {
				t.Errorf("computeHunks() returned %d hunks, want %d", len(hunks), tt.expectedHunks)
			}

			totalAdd := 0
			totalRemove := 0
			for _, hunk := range hunks {
				for _, line := range hunk.Lines {
					if line.Type == LineAdded {
						totalAdd++
					} else if line.Type == LineRemoved {
						totalRemove++
					}
				}
			}

			if totalAdd != tt.expectedAdd {
				t.Errorf("computeHunks() total added = %d, want %d", totalAdd, tt.expectedAdd)
			}
			if totalRemove != tt.expectedRemove {
				t.Errorf("computeHunks() total removed = %d, want %d", totalRemove, tt.expectedRemove)
			}
		})
	}
}

func TestComputeHunksLineNumbers(t *testing.T) {
	oldLines := []string{"line1", "line2", "line3", "line4"}
	newLines := []string{"line1", "line2 modified", "line3", "line4"}

	hunks, err := computeHunks(oldLines, newLines)
	if err != nil {
		t.Fatalf("computeHunks() error = %v", err)
	}

	if len(hunks) != 1 {
		t.Fatalf("computeHunks() returned %d hunks, want 1", len(hunks))
	}

	hunk := hunks[0]

	// Check old start and count
	if hunk.OldStart != 2 {
		t.Errorf("hunk.OldStart = %d, want 2", hunk.OldStart)
	}

	if hunk.OldCount < 1 {
		t.Errorf("hunk.OldCount = %d, want >= 1", hunk.OldCount)
	}

	// Check new start and count
	if hunk.NewStart != 2 {
		t.Errorf("hunk.NewStart = %d, want 2", hunk.NewStart)
	}

	if hunk.NewCount < 1 {
		t.Errorf("hunk.NewCount = %d, want >= 1", hunk.NewCount)
	}

	// Verify lines don't have trailing newlines
	for _, line := range hunk.Lines {
		if len(line.Content) > 0 && line.Content[len(line.Content)-1] == '\n' {
			t.Errorf("Line content has trailing newline: %q", line.Content)
		}
	}
}

func TestComputeHunksNoEmptyLines(t *testing.T) {
	oldLines := []string{"a", "b"}
	newLines := []string{"a", "b", "c"}

	hunks, err := computeHunks(oldLines, newLines)
	if err != nil {
		t.Fatalf("computeHunks() error = %v", err)
	}

	// Check that no line content is an empty string (unless intentional)
	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			if line.Content == "" && line.Type != LineContext {
				// Empty context lines are OK for context, but not for add/remove
				t.Errorf("Found empty line content in hunk: %+v", line)
			}
		}
	}
}

func TestComputeHunksWithContextIncludesLeadingLines(t *testing.T) {
	oldLines := []string{"a", "b", "c", "d", "e"}
	newLines := []string{"a", "b", "X", "d", "e"}

	hunks, err := computeHunksWithContext(oldLines, newLines, 1)
	if err != nil {
		t.Fatalf("computeHunksWithContext() error = %v", err)
	}

	if len(hunks) != 1 {
		t.Fatalf("computeHunksWithContext() returned %d hunks, want 1", len(hunks))
	}

	hunk := hunks[0]
	if hunk.OldStart != 2 || hunk.NewStart != 2 {
		t.Fatalf("hunk starts = old:%d new:%d, want old:2 new:2", hunk.OldStart, hunk.NewStart)
	}

	if len(hunk.Lines) < 3 {
		t.Fatalf("hunk line count = %d, want at least 3", len(hunk.Lines))
	}

	if hunk.Lines[0].Type != LineContext || hunk.Lines[0].Content != "b" {
		t.Fatalf("first line = (%v, %q), want context \"b\"", hunk.Lines[0].Type, hunk.Lines[0].Content)
	}
}

func TestComputeHunksWithLargeContextBehavesLikeWholeFile(t *testing.T) {
	oldLines := []string{"a", "b", "c", "d"}
	newLines := []string{"a", "B", "c", "d"}

	hunks, err := computeHunksWithContext(oldLines, newLines, WholeFileContext)
	if err != nil {
		t.Fatalf("computeHunksWithContext() error = %v", err)
	}

	if len(hunks) != 1 {
		t.Fatalf("computeHunksWithContext() returned %d hunks, want 1", len(hunks))
	}

	hunk := hunks[0]
	if hunk.OldStart != 1 || hunk.NewStart != 1 {
		t.Fatalf("hunk starts = old:%d new:%d, want old:1 new:1", hunk.OldStart, hunk.NewStart)
	}

	containsA := false
	for _, line := range hunk.Lines {
		if line.Type == LineContext && line.Content == "a" {
			containsA = true
			break
		}
	}
	if !containsA {
		t.Fatalf("expected leading unchanged line \"a\" in whole-file context hunk")
	}
}

// TestNewGitService tests creating a new GitService
func TestNewGitService(t *testing.T) {
	gitService := setupGitService(t)

	if gitService == nil {
		t.Fatal("NewGitService() returned nil")
	}

	if gitService.GetRepository() == nil {
		t.Error("GitService.Repository is nil")
	}
}

// TestGetRootPath tests getting the repository root path
func TestGetRootPath(t *testing.T) {
	gitService := setupGitService(t)

	rootPath, err := gitService.GetRootPath()
	if err != nil {
		t.Fatalf("GetRootPath() error = %v", err)
	}

	if rootPath == "" {
		t.Error("GetRootPath() returned empty string")
	}

	// Verify the path is absolute
	if !filepath.IsAbs(rootPath) {
		t.Errorf("GetRootPath() returned relative path: %s", rootPath)
	}
}

// TestGetCurrentBranch tests getting the current branch name
func TestGetCurrentBranch(t *testing.T) {
	gitService := setupGitService(t)

	branch, err := gitService.GetCurrentBranch()
	if err != nil {
		t.Fatalf("GetCurrentBranch() error = %v", err)
	}

	if branch == "" {
		t.Error("GetCurrentBranch() returned empty string")
	}

	// Branch name should not contain newline
	if len(branch) > 0 && branch[len(branch)-1] == '\n' {
		t.Errorf("Branch name has trailing newline: %q", branch)
	}
}

// TestGetChangedFiles tests getting the list of changed files
func TestGetChangedFiles(t *testing.T) {
	gitService := setupGitService(t)

	// Test both modes
	modes := []DiffMode{Unstaged, Staged}

	for _, mode := range modes {
		t.Run(mode.String(), func(t *testing.T) {
			files, err := gitService.GetChangedFiles(mode)
			if err != nil {
				t.Fatalf("GetChangedFiles(%v) error = %v", mode, err)
			}

			// Files should be sorted
			for i := 1; i < len(files); i++ {
				if files[i-1].Path > files[i].Path {
					t.Errorf("Files not sorted: %s comes before %s", files[i-1].Path, files[i].Path)
				}
			}

			// All files should have a path
			for _, file := range files {
				if file.Path == "" {
					t.Error("Found file with empty path")
				}
			}
		})
	}
}

// TestGetDiff tests getting diffs for changed files
func TestGetDiff(t *testing.T) {
	gitService := setupGitService(t)
	logger, err := NewLogger(INFO, "") // Create a test logger
	if err != nil {
		t.Logf("logger fallback in tests: %v", err)
	}

	// Create a temporary test file
	tmpFile := "test_temp_file.txt"
	defer os.Remove(tmpFile)

	// Write initial content
	content1 := "line1\nline2\nline3\n"
	if err := os.WriteFile(tmpFile, []byte(content1), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Add file to git
	wt, err := gitService.GetRepository().Worktree()
	if err != nil {
		t.Skip("Skipping test: cannot get worktree")
	}
	_, err = wt.Add(tmpFile)
	if err != nil {
		t.Skip("Skipping test: cannot add file to git")
	}

	// Get staged diffs
	diffs, err := gitService.GetDiff(Staged, DiffOnly, logger)
	if err != nil {
		t.Fatalf("GetDiff(Staged) error = %v", err)
	}

	// Should have at least one file (our test file)
	found := false
	for _, diff := range diffs {
		if diff.Path == tmpFile {
			found = true
			// Verify the diff has the expected structure
			if len(diff.Hunks) == 0 {
				t.Errorf("File %s has no hunks", diff.Path)
			}
			break
		}
	}

	if !found {
		t.Errorf("Test file %s not found in diffs", tmpFile)
	}
}

func TestGetUnifiedBranchCompareDiffSkipsLargeUntrackedFile(t *testing.T) {
	gitService := setupGitService(t)
	logger, err := NewLogger(INFO, "")
	if err != nil {
		t.Logf("logger fallback in tests: %v", err)
	}

	largeFile := "test_large_untracked_file.tmp"
	defer os.Remove(largeFile)

	content := bytes.Repeat([]byte("x"), MaxFileSize+1)
	if err := os.WriteFile(largeFile, content, 0644); err != nil {
		t.Fatalf("failed to create large test file: %v", err)
	}

	diffs, err := gitService.GetUnifiedBranchCompareDiff(DiffOnly, DefaultDiffContext, logger)
	if err != nil {
		t.Fatalf("GetUnifiedBranchCompareDiff should skip large files, got error: %v", err)
	}

	for _, d := range diffs {
		if d.Path == largeFile {
			t.Fatalf("large file %s should be skipped from branch compare diffs", largeFile)
		}
	}
}

// BenchmarkComputeHunks benchmarks the computeHunks function
func BenchmarkComputeHunks(b *testing.B) {
	oldLines := []string{}
	newLines := []string{}
	for i := 0; i < 1000; i++ {
		oldLines = append(oldLines, "line content")
	}
	// Modify middle line
	newLines = make([]string, len(oldLines))
	copy(newLines, oldLines)
	newLines[500] = "modified line content"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := computeHunks(oldLines, newLines)
		if err != nil {
			b.Fatalf("computeHunks() error = %v", err)
		}
	}
}

// Helper function to get DiffMode string representation
func (d DiffMode) String() string {
	switch d {
	case Unstaged:
		return "Unstaged"
	case Staged:
		return "Staged"
	default:
		return "Unknown"
	}
}

// TestHunkStructure verifies that hunks are properly structured
func TestHunkStructure(t *testing.T) {
	oldLines := []string{"context1", "remove1", "context2", "add1", "context3"}
	newLines := []string{"context1", "context2", "add1", "add2", "context3"}

	hunks, err := computeHunks(oldLines, newLines)
	if err != nil {
		t.Fatalf("computeHunks() error = %v", err)
	}

	for i, hunk := range hunks {
		// Each hunk should have at least one line
		if len(hunk.Lines) == 0 {
			t.Errorf("Hunk %d has no lines", i)
		}

		// OldStart and NewStart should be positive
		if hunk.OldStart < 1 {
			t.Errorf("Hunk %d has invalid OldStart: %d", i, hunk.OldStart)
		}
		if hunk.NewStart < 1 {
			t.Errorf("Hunk %d has invalid NewStart: %d", i, hunk.NewStart)
		}

		// OldCount and NewCount should be non-negative
		if hunk.OldCount < 0 {
			t.Errorf("Hunk %d has negative OldCount: %d", i, hunk.OldCount)
		}
		if hunk.NewCount < 0 {
			t.Errorf("Hunk %d has negative NewCount: %d", i, hunk.NewCount)
		}

		// Verify lines are in correct order
		hasRemovalAfterAddition := false
		for j := 1; j < len(hunk.Lines); j++ {
			if hunk.Lines[j-1].Type == LineAdded && hunk.Lines[j].Type == LineRemoved {
				hasRemovalAfterAddition = true
				break
			}
		}
		if hasRemovalAfterAddition {
			t.Logf("Warning: Hunk %d has removal after addition (this can happen in some cases)", i)
		}
	}
}
