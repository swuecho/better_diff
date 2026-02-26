package main

import "testing"

func TestComputeHunksWithDifflib_Parity(t *testing.T) {
	testCases := []struct {
		name    string
		old     []string
		new     []string
		context int
	}{
		{
			name:    "single line replace",
			old:     []string{"a", "b", "c"},
			new:     []string{"a", "x", "c"},
			context: 1,
		},
		{
			name:    "insert block",
			old:     []string{"a", "b", "c"},
			new:     []string{"a", "b", "x", "y", "c"},
			context: 2,
		},
		{
			name:    "delete block",
			old:     []string{"a", "b", "x", "y", "c"},
			new:     []string{"a", "b", "c"},
			context: 2,
		},
		{
			name:    "multiple hunks",
			old:     []string{"a", "b", "c", "d", "e", "f", "g"},
			new:     []string{"a", "B", "c", "d", "E", "f", "g"},
			context: 1,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			baseline, err := computeHunksWithContext(tc.old, tc.new, tc.context)
			if err != nil {
				t.Fatalf("baseline computeHunksWithContext() error = %v", err)
			}

			poc, err := computeHunksWithDifflib(tc.old, tc.new, tc.context)
			if err != nil {
				t.Fatalf("computeHunksWithDifflib() error = %v", err)
			}

			if countDiffLinesByType(baseline, LineAdded) != countDiffLinesByType(poc, LineAdded) {
				t.Fatalf("added lines mismatch: baseline=%d poc=%d", countDiffLinesByType(baseline, LineAdded), countDiffLinesByType(poc, LineAdded))
			}
			if countDiffLinesByType(baseline, LineRemoved) != countDiffLinesByType(poc, LineRemoved) {
				t.Fatalf("removed lines mismatch: baseline=%d poc=%d", countDiffLinesByType(baseline, LineRemoved), countDiffLinesByType(poc, LineRemoved))
			}
		})
	}
}

func countDiffLinesByType(hunks []Hunk, lineType LineType) int {
	count := 0
	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			if line.Type == lineType {
				count++
			}
		}
	}
	return count
}
