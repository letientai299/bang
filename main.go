// Command bang resolves address-bar shortcuts. It is meant to be set as the
// browser's default search engine, so it must never fail closed: anything it
// does not recognise is forwarded to a real search engine unchanged.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/taile/bang/internal/config"
	"github.com/taile/bang/internal/web"
)

func main() {
	path := flag.String("config", defaultConfigPath(), "path to config.yaml")
	// Off by default: as the default search engine this process sees every
	// query typed into the address bar, and none of it is worth writing down.
	verbose := flag.Bool("v", false, "log every query and where it resolved")
	flag.Parse()

	var live atomic.Pointer[config.Config]

	cfg, err := config.Load(*path)
	if err != nil {
		// Degrade to a plain search proxy rather than refusing to start; a
		// broken config must not leave the address bar unusable.
		log.Printf("config: %v — starting in fallback-only mode", err)
		cfg = config.SafeFallback()
	}
	live.Store(cfg)
	addr := cfg.Listen

	go config.Watch(*path, func() {
		next, err := config.Load(*path)
		if err != nil {
			log.Printf("reload: %v — keeping previous config", err)
			return
		}
		if next.Listen != addr {
			// The listener is bound once, at startup. Without this the new
			// address looks applied but silently does nothing.
			log.Printf(
				"reload: listen is now %s but still bound to %s — restart to rebind",
				next.Listen, addr,
			)
		}
		live.Store(next)
		log.Printf("reload: %d rules", len(next.Rules))
	})

	log.Printf(
		"listening on http://%s (%d rules) — open it to finish setup",
		addr, len(cfg.Rules),
	)
	log.Fatal((&http.Server{
		Addr:              addr,
		Handler:           web.Handler(&live, *verbose),
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
