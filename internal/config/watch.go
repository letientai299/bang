package config

import (
	"log"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounce is how long to wait for a save to settle. Editors emit several
// events per save, and one save should be one reload.
const debounce = 100 * time.Millisecond

// Watch calls onChange when the config file at path is written. It blocks, so
// callers run it in a goroutine.
//
// It watches the parent directory rather than the file: editors save via
// atomic rename, which replaces the inode and silently detaches a file-level
// watch. A watch that cannot be set up disables hot reload rather than
// returning an error — the already-loaded config keeps serving either way.
func Watch(path string, onChange func()) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("watch: %v — hot reload disabled", err)
		return
	}
	defer func() { _ = w.Close() }()

	// Follow symlinks so a config kept in a git repo and linked into place
	// still reloads: writes land in the repo directory, not the link's.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	dir := filepath.Dir(path)
	if err := w.Add(dir); err != nil {
		log.Printf("watch %s: %v — hot reload disabled", dir, err)
		return
	}

	var timer *time.Timer
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if filepath.Clean(ev.Name) != filepath.Clean(path) {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, onChange)
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("watch: %v", err)
		}
	}
}
