package main

import (
	"errors"
	"os"
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
	watcher     *fsnotify.Watcher
	gitPath     string
	isWatching  bool
}

// NewWatcher creates a new file system watcher
func NewWatcher(gitPath string) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		watcher:    fsWatcher,
		gitPath:    gitPath + "/.git",
		isWatching: false,
	}

	// Add key .git directories to watch
	dirsToWatch := []string{
		w.gitPath,
		w.gitPath + "/HEAD",
		w.gitPath + "/index",
		w.gitPath + "/refs/heads",
		w.gitPath + "/refs/tags",
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
