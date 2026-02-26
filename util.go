package main

import (
	"io"
	pathpkg "path"
	"sort"
	"strings"
)

func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

func splitPath(inputPath string) []string {
	rawParts := strings.Split(inputPath, "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func joinPath(parts []string) string {
	return strings.Join(parts, "/")
}

func fileNameFromPath(path string) string {
	name := pathpkg.Base(path)
	if name != "." && name != "/" && name != "" {
		return name
	}
	return path
}

func clamp(value, lower, upper int) int {
	if value < lower {
		return lower
	}
	if value > upper {
		return upper
	}
	return value
}

func panelContentHeight(height int) int {
	return max(0, height-2)
}

func visibleRange(start, window, length int) (int, int) {
	start = clamp(start, 0, length)
	end := min(start+window, length)
	return start, end
}

func copyMap(m map[string]int) map[string]int {
	result := make(map[string]int, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func sortedFieldKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
