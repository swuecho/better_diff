package main

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
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
	dmp := diffmatchpatch.New()

	if contextLines < 0 {
		contextLines = 0
	}

	// Join lines with a trailing newline sentinel so pure append/prepend edits
	// are not misclassified as replace operations on the final line.
	oldText := joinLinesForDiff(oldLines)
	newText := joinLinesForDiff(newLines)

	// Convert to line-based character encoding
	// This encodes each unique line as a single character for efficient diffing
	oldChars, newChars, lineArray := dmp.DiffLinesToChars(oldText, newText)

	// Compute the diff on the character-encoded text
	charDiffs := dmp.DiffMain(oldChars, newChars, false)

	// Convert character diffs to line diffs manually
	// We need to split the character-encoded text by characters and map each to a line
	// Then merge adjacent diffs of the same type
	type lineDiff struct {
		Type  diffmatchpatch.Operation
		Lines []string
	}

	var lineDiffs []lineDiff

	for _, charDiff := range charDiffs {
		// Each character in charDiff.Text represents one line
		runes := []rune(charDiff.Text)
		var lines []string
		for _, r := range runes {
			// Convert rune to int for array indexing
			idx := int(r)
			if idx < len(lineArray) {
				// Get the line and strip trailing newline if present
				line := lineArray[idx]
				line = strings.TrimSuffix(line, "\n")
				lines = append(lines, line)
			}
		}

		// Merge with previous diff if same type
		if len(lineDiffs) > 0 && lineDiffs[len(lineDiffs)-1].Type == charDiff.Type {
			lineDiffs[len(lineDiffs)-1].Lines = append(lineDiffs[len(lineDiffs)-1].Lines, lines...)
		} else {
			lineDiffs = append(lineDiffs, lineDiff{
				Type:  charDiff.Type,
				Lines: lines,
			})
		}
	}

	// Convert line diffs to hunks
	var hunks []Hunk
	var currentHunk *Hunk
	pendingEqual := []string{}

	oldLineNum := 1
	newLineNum := 1

	for _, diff := range lineDiffs {
		if len(diff.Lines) == 0 {
			continue
		}

		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			if currentHunk != nil {
				// Add context lines
				for _, line := range diff.Lines {
					currentHunk.Lines = append(currentHunk.Lines, DiffLine{
						Type:    LineContext,
						Content: line,
					})
					oldLineNum++
					newLineNum++
				}

				// Check if we should close the hunk and split around large unchanged regions.
				trailingContext := countTrailingContextLines(currentHunk.Lines)

				// Only close if we have actual changes and enough context
				if trailingContext >= contextLines {
					if hunkHasChanges(currentHunk.Lines) {
						trimCount := 0
						// Trim to keep only contextLines lines of trailing context
						if trailingContext > contextLines {
							trimCount = trailingContext - contextLines
							currentHunk.Lines = currentHunk.Lines[:len(currentHunk.Lines)-trimCount]
						}
						currentHunk.OldCount = oldLineNum - currentHunk.OldStart - trimCount
						currentHunk.NewCount = newLineNum - currentHunk.NewStart - trimCount

						hunks = append(hunks, *currentHunk)
						currentHunk = nil

						// Keep tail context as potential leading context for the next hunk.
						pendingEqual = tailContextLines(diff.Lines, contextLines)
					}
				}
			} else {
				oldLineNum += len(diff.Lines)
				newLineNum += len(diff.Lines)
				if contextLines > 0 {
					pendingEqual = append(pendingEqual, diff.Lines...)
					if len(pendingEqual) > contextLines {
						pendingEqual = tailContextLines(pendingEqual, contextLines)
					}
				} else {
					pendingEqual = pendingEqual[:0]
				}
			}

		case diffmatchpatch.DiffDelete:
			if currentHunk == nil {
				currentHunk = newHunkWithLeadingContext(pendingEqual, contextLines, oldLineNum, newLineNum, len(diff.Lines))
				pendingEqual = pendingEqual[:0]
			}

			for _, line := range diff.Lines {
				currentHunk.Lines = append(currentHunk.Lines, DiffLine{
					Type:    LineRemoved,
					Content: line,
				})
				oldLineNum++
			}

		case diffmatchpatch.DiffInsert:
			if currentHunk == nil {
				currentHunk = newHunkWithLeadingContext(pendingEqual, contextLines, oldLineNum, newLineNum, len(diff.Lines))
				pendingEqual = pendingEqual[:0]
			}

			for _, line := range diff.Lines {
				currentHunk.Lines = append(currentHunk.Lines, DiffLine{
					Type:    LineAdded,
					Content: line,
				})
				newLineNum++
			}
		}
	}

	// Close the last hunk
	if currentHunk != nil {
		currentHunk.OldCount = oldLineNum - currentHunk.OldStart
		currentHunk.NewCount = newLineNum - currentHunk.NewStart

		// Trim trailing context
		trailingContext := countTrailingContextLines(currentHunk.Lines)

		if trailingContext > contextLines {
			trimCount := trailingContext - contextLines
			currentHunk.Lines = currentHunk.Lines[:len(currentHunk.Lines)-trimCount]
			currentHunk.OldCount -= trimCount
			currentHunk.NewCount -= trimCount
		}

		hunks = append(hunks, *currentHunk)
	}

	return hunks, nil
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
