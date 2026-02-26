# Better Diff User Manual

## What It Is
`better_diff` is a terminal UI for reviewing Git changes with:
- A file tree (left panel)
- A diff viewer (right panel)
- Live auto-refresh when repository files or Git metadata change

It is designed to run inside a Git repository and show `unstaged`, `staged`, or `branch compare` changes.

## Requirements
- Go (to build from source)
- A local Git repository
- Terminal with enough width/height for split panels

## Install
From the project root:

```bash
go build -o better_diff
```

Optional install:

```bash
cp better_diff /usr/local/bin/
```

## Start the App
Run from inside any Git repository:

```bash
./better_diff
```

If launched outside a Git repo, startup fails with an error.

## Screen Layout
- Header: app name, current branch, repo path, current mode, view mode, total file/line stats
- Main area:
  - `Diff Only` view: split file tree + diff panel
  - `Whole File` view: diff panel only
- Footer: contextual keyboard hints + diff scroll percentage (when diff panel is active)

## Modes
Press `s` to cycle:
1. `Unstaged`
2. `Staged`
3. `Branch Compare`

### Mode Details
- `Unstaged`: working tree vs index (includes untracked files)
- `Staged`: index vs HEAD (untracked files are not shown unless staged)
- `Branch Compare`: unified diff of current working tree vs default branch (`main`, `master`, or `develop` fallback logic)

## View Types
Press `f` to toggle:
- `Diff Only`:
  - Shows changed hunks with context lines
  - File tree visible
  - `Tab` switches active panel
- `Whole File`:
  - Shows near-full file content with diff highlighting
  - File tree hidden
  - `Tab` is disabled

## Keyboard Shortcuts
### Global
- `q` or `Ctrl+C`: quit
- `?`: show/hide help overlay
- `s`: cycle diff mode (`Unstaged` -> `Staged` -> `Branch Compare`)
- `f`: toggle `Diff Only` / `Whole File`

### File Tree Panel
- `Up` or `k`: move selection up
- `Down` or `j`: move selection down
- `PgUp` / `PgDn`: page up/down
- `Enter` or `Space`:
  - On folder: expand/collapse
  - On file: load/select diff for that file

### Diff Panel
- `Up` / `Down`: scroll line-by-line
- `PgUp` / `PgDn`: scroll page-by-page
- `gg`: jump to top
- `G`: jump to bottom

In `Diff Only` mode:
- `j`: jump to next hunk
- `k`: jump to previous hunk
- `o`: increase diff context (adds 5 more context lines each press)
- `O`: reset context back to default (5 lines)

In `Whole File` mode:
- `j` / `k`: scroll down/up (not hunk-jump)

### Panel Switching
- `Tab`: switch between file tree and diff panel (Diff Only mode only)

## File Tree Indicators
- `▶` collapsed directory
- `▼` expanded directory
- `●` modified
- `+` added
- `-` deleted

Per-file line stats appear next to file names (for example `+12/-3`).

## Live Refresh
The app watches:
- Working tree directories (recursively, excluding `.git`)
- Key `.git` paths (`HEAD`, `index`, refs)

When changes are detected, file list and diffs auto-reload for the current mode/view.

## Limits and Behavior Notes
- Maximum file size for diff processing: 10 MB per file
- Files above limit are skipped and logged as warnings/errors
- Binary or unparsable diff content may show as:
  - `No diff content available (binary file or no changes)`
- No command-line flags are implemented (`--help`, `-h`, etc.)

## Troubleshooting
- `failed to open git repository`:
  - Start the app from inside a Git repository
- Empty list in `Staged` mode:
  - Stage files first (`git add ...`)
- Nothing in `Branch Compare`:
  - You may be on default branch or have no differences vs default branch + working tree
- Unexpectedly missing large file diff:
  - Check if file exceeds 10 MB limit
