package main

import (
	"errors"
	"fmt"
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

const watcherDebounceDelay = 50 * time.Millisecond

var (
	errWatcherNotRunning = errors.New("watcher is not running")
	errWatcherClosed     = errors.New("watcher closed")
)

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
		if closeErr := fsWatcher.Close(); closeErr != nil {
			return nil, fmt.Errorf("initialize watcher: %w (close watcher: %v)", err, closeErr)
		}
		return nil, fmt.Errorf("initialize watcher: %w", err)
	}

	// Add key .git paths to watch.
	gitPathsToWatch := []string{
		w.gitPath,
		filepath.Join(w.gitPath, "HEAD"),
		filepath.Join(w.gitPath, "index"),
		filepath.Join(w.gitPath, "refs"),
		filepath.Join(w.gitPath, "refs", "heads"),
		filepath.Join(w.gitPath, "refs", "tags"),
	}

	for _, path := range gitPathsToWatch {
		addWatchPathIfExists(fsWatcher, path)
	}

	w.isWatching = true
	return w, nil
}

// WaitForChange waits for the next file system change
func (w *Watcher) WaitForChange() tea.Cmd {
	return func() tea.Msg {
		if !w.isWatching {
			return errMsg{errWatcherNotRunning}
		}

		// Wait for an event
		for {
			select {
			case event, ok := <-w.watcher.Events:
				if !ok {
					return errMsg{errWatcherClosed}
				}

				if !isRelevantWatchEvent(event.Op) {
					continue
				}
				w.trackCreatedDirectory(event)
				time.Sleep(watcherDebounceDelay)
				return FSChangeMsg{time: time.Now()}

			case err, ok := <-w.watcher.Errors:
				if !ok {
					return errMsg{errWatcherClosed}
				}
				return errMsg{err}
			}
		}
	}
}

func addWatchPathIfExists(fsWatcher *fsnotify.Watcher, path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	if err := fsWatcher.Add(path); err != nil {
		// Best effort: ignore individual path failures.
		return
	}
}

func isRelevantWatchEvent(op fsnotify.Op) bool {
	return op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0
}

func (w *Watcher) trackCreatedDirectory(event fsnotify.Event) {
	if event.Op&fsnotify.Create == 0 {
		return
	}

	info, err := os.Stat(event.Name)
	if err != nil || !info.IsDir() {
		return
	}

	// Best effort: keep existing watch state even if this directory cannot be added.
	_ = w.addRecursiveDirs(event.Name)
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
