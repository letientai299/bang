// Command bang resolves address-bar shortcuts. It is meant to be set as the
// browser's default search engine, so it must never fail closed: anything it
// does not recognise is forwarded to a real search engine unchanged.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/example/bang/internal/config"
	"github.com/example/bang/internal/web"
)

func main() {
	// The subcommand is taken before flag parsing: the flag package stops at
	// the first non-flag argument, so "bang check path" would otherwise parse
	// as a serve invocation carrying a stray argument.
	if len(os.Args) > 1 && os.Args[1] == "check" {
		os.Exit(check(os.Args[2:]))
	}

	path := flag.String("config", defaultConfigPath(), "path to config.yaml")
	// Off by default: as the default search engine this process sees every
	// query typed into the address bar, and none of it is worth writing down.
	verbose := flag.Bool("v", false, "log every query and where it resolved")
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() > 0 {
		// "bang -config x check" would otherwise start the server and never
		// run the check the caller asked for.
		fmt.Fprintf(os.Stderr, "unexpected argument %q\n", flag.Arg(0))
		usage()
		os.Exit(2)
	}

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

func usage() {
	// os.Stderr rather than flag.CommandLine.Output(), which is the same writer
	// here and is what PrintDefaults below uses.
	fmt.Fprint(os.Stderr, "usage:\n"+
		"  bang [flags]            serve the resolver\n"+
		"  bang check <config...>  load each config, report what is wrong, exit\n"+
		"\nflags:\n")
	flag.PrintDefaults()
}

// check validates rule sets without binding anything, so a config kept in git
// can be verified by a commit hook or a CI job rather than by typing into a
// browser and watching where it lands. Every path given is checked, because a
// hook hands its tool every file that matched — stopping at the first would
// pass a second broken config in silence.
//
// The path is required rather than defaulted: a hook or a job means to check
// the file it names, and silently checking the installed config instead would
// report a pass for something it never looked at.
func check(paths []string) int {
	if len(paths) == 0 || strings.HasPrefix(paths[0], "-") {
		usage()
		switch {
		case len(paths) == 0:
			return 2
		case paths[0] == "-h", paths[0] == "-help", paths[0] == "--help":
			return 0
		}
		return 2
	}

	status := 0
	for _, path := range paths {
		c, err := config.Load(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			status = 1
			continue
		}
		fmt.Printf("%s: ok, %d rules\n", path, len(c.Rules))
	}
	return status
}

// defaultConfigPath follows the XDG base directory spec on every platform.
// os.UserConfigDir would send macOS to ~/Library/Application Support, which is
// neither where the install task writes the config nor where the docs say it
// lives, so bang would look somewhere nothing had put a file.
func defaultConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.yaml"
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "bang", "config.yaml")
}
