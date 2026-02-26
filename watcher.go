package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// FSChangeMsg is sent when file system changes are detected
type FSChangeMsg struct {
	time time.Time
}

// Watcher handles file system watching
type Watcher struct {
	watcher    *fsnotify.Watcher
	rootPath   string
	gitPath    string
	isWatching bool
}

// NewWatcher creates a new file system watcher
func NewWatcher(rootPath string) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		watcher:    fsWatcher,
		rootPath:   rootPath,
		gitPath:    filepath.Join(rootPath, ".git"),
		isWatching: false,
	}

	// Watch working tree recursively (except .git) so unstaged edits are detected.
	if err := w.addRecursiveDirs(w.rootPath); err != nil {
		_ = fsWatcher.Close()
		return nil, err
	}

	// Add key .git directories to watch
	dirsToWatch := []string{
		w.gitPath,
		filepath.Join(w.gitPath, "HEAD"),
		filepath.Join(w.gitPath, "index"),
		filepath.Join(w.gitPath, "refs"),
		filepath.Join(w.gitPath, "refs", "heads"),
		filepath.Join(w.gitPath, "refs", "tags"),
	}

	for _, dir := range dirsToWatch {
		if _, err := os.Stat(dir); err == nil {
			if fsWatcher.Add(dir) != nil {
				// Try to add, ignore errors for individual paths
				continue
			}
		}
	}

	w.isWatching = true
	return w, nil
}

// WaitForChange waits for the next file system change
func (w *Watcher) WaitForChange() tea.Cmd {
	return func() tea.Msg {
		if !w.isWatching {
			return errMsg{errors.New("watcher is not running")}
		}

		// Wait for an event
		for {
			select {
			case event, ok := <-w.watcher.Events:
				if !ok {
					return errMsg{errors.New("watcher closed")}
				}

				// Filter for relevant events
				if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
					// Track newly created directories so deep file changes are observed.
					if event.Op&fsnotify.Create != 0 {
						if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
							_ = w.addRecursiveDirs(event.Name)
						}
					}
					// Add a small debounce to handle rapid changes
					time.Sleep(50 * time.Millisecond)
					return FSChangeMsg{time: time.Now()}
				}

			case err, ok := <-w.watcher.Errors:
				if !ok {
					return errMsg{errors.New("watcher closed")}
				}
				return errMsg{err}
			}
		}
	}
}

// Close closes the file system watcher
func (w *Watcher) Close() error {
	if !w.isWatching {
		return nil
	}

	w.isWatching = false
	return w.watcher.Close()
}

func (w *Watcher) addRecursiveDirs(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if w.isGitDir(path) {
			return filepath.SkipDir
		}
		if err := w.watcher.Add(path); err != nil {
			return nil
		}
		return nil
	})
}

func (w *Watcher) isGitDir(path string) bool {
	if path == w.gitPath {
		return true
	}
	return strings.HasPrefix(path, w.gitPath+string(os.PathSeparator))
}
