package main

import (
	"strings"
)

// ChangeType represents the type of change
type ChangeType int

const (
	Modified ChangeType = iota
	Added
	Deleted
	Renamed
)

// FileDiff represents a file with its changes
type FileDiff struct {
	Path         string
	OldPath      string // for renames
	ChangeType   ChangeType
	Hunks        []Hunk
	LinesAdded   int
	LinesRemoved int
}

// Commit represents a git commit
type Commit struct {
	Hash      string
	ShortHash string
	Author    string
	Message   string
	Date      string
}

// Hunk represents a section of changes
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []DiffLine
}

// DiffLine represents a single line in a diff
type DiffLine struct {
	Type    LineType
	Content string
	LineNum int // 0 for context lines
}

// LineType represents the type of line in a diff
type LineType int

const (
	LineContext LineType = iota
	LineAdded
	LineRemoved
)

// DiffMode represents whether we're showing staged or unstaged changes
type DiffMode int

const (
	Unstaged DiffMode = iota
	Staged
	BranchCompare
)

// DiffViewMode represents how much context to show in diff
type DiffViewMode int

const (
	DiffOnly DiffViewMode = iota
	WholeFile
)

const (
	// DefaultDiffContext is the default number of context lines in diff
	DefaultDiffContext = 5
	// WholeFileContext is a large number to show whole file in diff
	WholeFileContext = 999999
	// MaxFileSize is the maximum file size we'll diff (10MB)
	MaxFileSize = 10 * 1024 * 1024
)

// computeHunks computes hunks with default grouping behavior and normalizes
// hunk headers to start at the first changed line.
func computeHunks(oldLines, newLines []string) ([]Hunk, error) {
	hunks, err := computeHunksWithContext(oldLines, newLines, DefaultDiffContext)
	if err != nil {
		return nil, err
	}

	normalized := make([]Hunk, 0, len(hunks))
	for _, h := range hunks {
		h = trimLeadingContext(h)
		normalized = append(normalized, h)
	}

	return normalized, nil
}

// computeHunksWithContext computes diff hunks using Myers diff algorithm with a custom context size.
func computeHunksWithContext(oldLines, newLines []string, contextLines int) ([]Hunk, error) {
	return computeHunksWithDifflib(oldLines, newLines, contextLines)
}

type lineDiff struct {
	Type  diffOp
	Lines []string
}

type diffOp int

const (
	diffEqual diffOp = iota
	diffDelete
	diffInsert
)

func appendMergedLineDiff(lineDiffs []lineDiff, diffType diffOp, lines []string) []lineDiff {
	if len(lineDiffs) > 0 && lineDiffs[len(lineDiffs)-1].Type == diffType {
		lineDiffs[len(lineDiffs)-1].Lines = append(lineDiffs[len(lineDiffs)-1].Lines, lines...)
		return lineDiffs
	}
	return append(lineDiffs, lineDiff{
		Type:  diffType,
		Lines: lines,
	})
}

type hunkBuilder struct {
	contextLines   int
	hunks          []Hunk
	currentHunk    *Hunk
	pendingContext []string
	oldLineNum     int
	newLineNum     int
}

func buildHunks(lineDiffs []lineDiff, contextLines int) []Hunk {
	builder := hunkBuilder{
		contextLines:   contextLines,
		hunks:          make([]Hunk, 0, len(lineDiffs)),
		pendingContext: []string{},
		oldLineNum:     1,
		newLineNum:     1,
	}

	for _, diff := range lineDiffs {
		builder.process(diff)
	}

	builder.finalize()
	return builder.hunks
}

func (b *hunkBuilder) process(diff lineDiff) {
	switch diff.Type {
	case diffEqual:
		b.processEqualLines(diff.Lines)
	case diffDelete:
		b.processRemovedLines(diff.Lines)
	case diffInsert:
		b.processAddedLines(diff.Lines)
	}
}

func (b *hunkBuilder) processEqualLines(lines []string) {
	if b.currentHunk == nil {
		b.oldLineNum += len(lines)
		b.newLineNum += len(lines)
		b.pendingContext = appendPendingContext(b.pendingContext, lines, b.contextLines)
		return
	}

	for _, line := range lines {
		b.currentHunk.Lines = append(b.currentHunk.Lines, DiffLine{
			Type:    LineContext,
			Content: line,
		})
		b.oldLineNum++
		b.newLineNum++
	}

	b.maybeCloseCurrentHunk(lines)
}

func (b *hunkBuilder) processRemovedLines(lines []string) {
	b.ensureCurrentHunk(len(lines))
	for _, line := range lines {
		b.currentHunk.Lines = append(b.currentHunk.Lines, DiffLine{
			Type:    LineRemoved,
			Content: line,
		})
		b.oldLineNum++
	}
}

func (b *hunkBuilder) processAddedLines(lines []string) {
	b.ensureCurrentHunk(len(lines))
	for _, line := range lines {
		b.currentHunk.Lines = append(b.currentHunk.Lines, DiffLine{
			Type:    LineAdded,
			Content: line,
		})
		b.newLineNum++
	}
}

func (b *hunkBuilder) ensureCurrentHunk(diffLineCount int) {
	if b.currentHunk != nil {
		return
	}
	b.currentHunk = newHunkWithLeadingContext(
		b.pendingContext,
		b.contextLines,
		b.oldLineNum,
		b.newLineNum,
		diffLineCount,
	)
	b.pendingContext = b.pendingContext[:0]
}

func (b *hunkBuilder) maybeCloseCurrentHunk(equalLines []string) {
	trailingContext := countTrailingContextLines(b.currentHunk.Lines)
	if trailingContext < b.contextLines || !hunkHasChanges(b.currentHunk.Lines) {
		return
	}

	trimCount := max(0, trailingContext-b.contextLines)
	if trimCount > 0 {
		b.currentHunk.Lines = b.currentHunk.Lines[:len(b.currentHunk.Lines)-trimCount]
	}

	b.currentHunk.OldCount = b.oldLineNum - b.currentHunk.OldStart - trimCount
	b.currentHunk.NewCount = b.newLineNum - b.currentHunk.NewStart - trimCount
	b.hunks = append(b.hunks, *b.currentHunk)
	b.currentHunk = nil
	b.pendingContext = tailContextLines(equalLines, b.contextLines)
}

func (b *hunkBuilder) finalize() {
	if b.currentHunk == nil {
		return
	}

	b.currentHunk.OldCount = b.oldLineNum - b.currentHunk.OldStart
	b.currentHunk.NewCount = b.newLineNum - b.currentHunk.NewStart

	trailingContext := countTrailingContextLines(b.currentHunk.Lines)
	if trailingContext > b.contextLines {
		trimCount := trailingContext - b.contextLines
		b.currentHunk.Lines = b.currentHunk.Lines[:len(b.currentHunk.Lines)-trimCount]
		b.currentHunk.OldCount -= trimCount
		b.currentHunk.NewCount -= trimCount
	}

	b.hunks = append(b.hunks, *b.currentHunk)
}

func appendPendingContext(pendingContext, lines []string, contextLines int) []string {
	if contextLines <= 0 {
		return pendingContext[:0]
	}

	pendingContext = append(pendingContext, lines...)
	if len(pendingContext) <= contextLines {
		return pendingContext
	}
	return tailContextLines(pendingContext, contextLines)
}

func trimLeadingContext(h Hunk) Hunk {
	leadCtx := 0
	for _, line := range h.Lines {
		if line.Type != LineContext {
			break
		}
		leadCtx++
	}

	if leadCtx == 0 {
		return h
	}

	h.Lines = h.Lines[leadCtx:]
	h.OldStart += leadCtx
	h.NewStart += leadCtx
	if h.OldCount >= leadCtx {
		h.OldCount -= leadCtx
	}
	if h.NewCount >= leadCtx {
		h.NewCount -= leadCtx
	}
	return h
}

func countTrailingContextLines(lines []DiffLine) int {
	count := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Type != LineContext {
			break
		}
		count++
	}
	return count
}

func hunkHasChanges(lines []DiffLine) bool {
	for _, line := range lines {
		if line.Type != LineContext {
			return true
		}
	}
	return false
}

func tailContextLines(lines []string, contextLines int) []string {
	if contextLines <= 0 {
		return nil
	}
	if len(lines) <= contextLines {
		return append([]string(nil), lines...)
	}
	return append([]string(nil), lines[len(lines)-contextLines:]...)
}

func newHunkWithLeadingContext(pendingEqual []string, contextLines int, oldLineNum, newLineNum, diffLineCount int) *Hunk {
	leadingContext := pendingEqual
	if contextLines <= 0 {
		leadingContext = nil
	}

	hunk := &Hunk{
		Lines:    make([]DiffLine, 0, len(leadingContext)+diffLineCount),
		OldStart: oldLineNum - len(leadingContext),
		NewStart: newLineNum - len(leadingContext),
	}

	for _, line := range leadingContext {
		hunk.Lines = append(hunk.Lines, DiffLine{
			Type:    LineContext,
			Content: line,
		})
	}

	return hunk
}

func joinLinesForDiff(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// splitLines splits content by newline and normalizes the result
// It removes the trailing empty string that results from splitting text with a trailing newline
// For example: "a\nb\n" -> ["a", "b"] instead of ["a", "b", ""]
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}

	lines := strings.Split(content, "\n")

	// Remove trailing empty string if present (from trailing newline)
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}
