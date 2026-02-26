package main

import (
	"fmt"

	"github.com/pmezard/go-difflib/difflib"
)

// computeHunksWithDifflib computes diff hunks using difflib.SequenceMatcher.
func computeHunksWithDifflib(oldLines, newLines []string, contextLines int) ([]Hunk, error) {
	normalizedContext := max(0, contextLines)
	matcher := difflib.NewMatcher(oldLines, newLines)
	opCodes := matcher.GetOpCodes()
	lineDiffs, err := opCodesToLineDiffs(opCodes, oldLines, newLines)
	if err != nil {
		return nil, err
	}
	return buildHunks(lineDiffs, normalizedContext), nil
}

func opCodesToLineDiffs(opCodes []difflib.OpCode, oldLines, newLines []string) ([]lineDiff, error) {
	lineDiffs := make([]lineDiff, 0, len(opCodes))

	for _, op := range opCodes {
		switch op.Tag {
		case 'e':
			lineDiffs = appendMergedLineDiff(lineDiffs, diffEqual, oldLines[op.I1:op.I2])
		case 'd':
			lineDiffs = appendMergedLineDiff(lineDiffs, diffDelete, oldLines[op.I1:op.I2])
		case 'i':
			lineDiffs = appendMergedLineDiff(lineDiffs, diffInsert, newLines[op.J1:op.J2])
		case 'r':
			if op.I1 < op.I2 {
				lineDiffs = appendMergedLineDiff(lineDiffs, diffDelete, oldLines[op.I1:op.I2])
			}
			if op.J1 < op.J2 {
				lineDiffs = appendMergedLineDiff(lineDiffs, diffInsert, newLines[op.J1:op.J2])
			}
		default:
			return nil, fmt.Errorf("unsupported opcode tag: %q", op.Tag)
		}
	}

	return lineDiffs, nil
}
