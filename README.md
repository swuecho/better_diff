# Better Diff - Terminal Git Diff Viewer

A beautiful terminal UI (TUI) for viewing git diffs, built with Go and BubbleTea.

## Features

- **File tree navigation**: Browse changed files in a collapsible tree structure
- **Diff highlighting**: Color-coded additions (green) and deletions (red)
- **Keyboard navigation**: Intuitive vim-style controls
- **Staged/Unstaged changes**: Toggle between working directory and staged changes
- **Split-panel view**: File tree on the left, diff view on the right

## Installation

```bash
# Clone the repository
git clone <repo-url>
cd better_diff

# Build the binary
go build -o better_diff

# (Optional) Install to system
cp better_diff /usr/local/bin/
```

## Usage

Run from any git repository:

```bash
./better_diff
```

### Keyboard Controls

| Key | Action |
|-----|--------|
| `↑` / `k` | Move up in file list |
| `↓` / `j` | Move down in file list |
| `Enter` / `Space` | Select file or expand/collapse directory |
| `Tab` | Switch between file tree and diff panels |
| `s` | Toggle between staged and unstaged changes |
| `q` / `Ctrl+C` | Quit |

### Visual Indicators

- `●` Yellow - Modified files
- `+` Green - Added files
- `-` Red - Deleted files
- `▶` Collapsed directory
- `▼` Expanded directory

## Project Structure

```
better_diff/
├── main.go       # Entry point and git repo check
├── model.go      # TUI model and state management
├── view.go       # UI rendering
├── update.go     # Event handling and navigation
└── git.go        # Git diff parsing
```

## Development

### Dependencies

- [BubbleTea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Styling
- [Bubbles](https://github.com/charmbracelet/bubbles) - UI components

### Building

```bash
go build -o better_diff
```

### Running

```bash
# Make some changes to files
echo "test" >> file.txt

# Run better_diff
./better_diff
```

## Screenshot

The UI features a split-panel layout:
- **Left panel**: File tree with change indicators
- **Right panel**: Line-by-line diff with syntax highlighting
- **Header**: Repository path and branch info
- **Footer**: Keyboard shortcuts

## License

MIT
