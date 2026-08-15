// bang resolves address-bar shortcuts. It is meant to be set as the browser's
// default search engine, so it must never fail closed: anything it does not
// recognise is forwarded to a real search engine unchanged.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

func main() {
	path := flag.String("config", defaultConfigPath(), "path to config.yaml")
	// Off by default: as the default search engine this process sees every
	// query typed into the address bar, and none of it is worth writing down.
	verbose := flag.Bool("v", false, "log every query and where it resolved")
	flag.Parse()

	var live atomic.Pointer[Config]

	cfg, err := LoadConfig(*path)
	if err != nil {
		// Degrade to a plain search proxy rather than refusing to start; a
		// broken config must not leave the address bar unusable.
		log.Printf("config: %v — starting in fallback-only mode", err)
		cfg = SafeFallback()
	}
	live.Store(cfg)

	go watch(*path, func() {
		next, err := LoadConfig(*path)
		if err != nil {
			log.Printf("reload: %v — keeping previous config", err)
			return
		}
		live.Store(next)
		log.Printf("reload: %d rules", len(next.Rules))
	})

	srv := &server{live: &live, verbose: *verbose}
	addr := cfg.Listen
	log.Printf("listening on http://%s (%d rules) — open it to finish setup", addr, len(cfg.Rules))
	log.Fatal((&http.Server{
		Addr:              addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}).ListenAndServe())
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(dir, "bang", "config.yaml")
}

// watch fires onChange when the config file is written. It watches the parent
// directory rather than the file: editors save via atomic rename, which
// replaces the inode and silently detaches a file-level watch.
func watch(path string, onChange func()) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("watch: %v — hot reload disabled", err)
		return
	}
	defer w.Close()

	// Follow symlinks so a config kept in a git repo and linked into place
	// still reloads: writes land in the repo directory, not the link's.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}

	dir := filepath.Dir(path)
	if err := w.Add(dir); err != nil {
		log.Printf("watch %s: %v — hot reload disabled", dir, err)
		return
	}

	// Editors emit several events per save; debounce so one save is one reload.
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
			timer = time.AfterFunc(100*time.Millisecond, onChange)
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("watch: %v", err)
		}
	}
}
