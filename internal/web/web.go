// Package web is bang's HTTP surface: the redirect the browser hits on every
// keystroke, the descriptor that installs it as a search engine, and the page
// that walks through the setup. Where a query actually goes is decided by
// internal/config.
package web

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"

	"github.com/taile/bang/internal/config"
)

// shortName must match the OpenSearch <ShortName> and the autodiscovery link's
// title attribute; Firefox rejects the descriptor if they differ. Max 16 chars.
const shortName = "bang"

type server struct {
	live    *atomic.Pointer[config.Config]
	verbose bool
}

// Handler serves the resolver against whichever config is live at request
// time. It reads live on every request rather than taking a *config.Config, so
// a reload takes effect without rebuilding the handler.
//
// verbose logs every query and where it resolved. It is off by default: as the
// default search engine this process sees everything typed into the address
// bar, and none of it is worth writing down.
func Handler(live *atomic.Pointer[config.Config], verbose bool) http.Handler {
	s := &server{live: live, verbose: verbose}

	mux := http.NewServeMux()
	// "GET /{$}" matches the root and nothing else. A bare "/" pattern is a
	// catch-all, so every stray path the browser probes — /favicon.ico,
	// /.well-known/… — used to render the onboarding page with a 200.
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /opensearch.xml", s.handleOpenSearch)
	mux.HandleFunc("GET /resolve", s.handleResolve)
	return loopbackOnly(mux)
}

// loopbackOnly rejects requests whose Host is not a loopback name. Binding
// 127.0.0.1 is not sufficient on its own: a remote page can reach a loopback
// service by DNS rebinding — pointing its own hostname at 127.0.0.1 — after
// which the browser treats the reply as same-origin and the page can read the
// whole rule set. The rebound request still carries the attacker's hostname in
// Host, so checking it closes the hole.
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "bang answers on loopback only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostPort string) bool {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	// SplitHostPort leaves the brackets on a bare IPv6 literal with no port.
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && addr.IsLoopback()
}

// baseURL reflects the host the browser actually used, so the descriptor works
// whether it was reached via 127.0.0.1 or localhost. Chrome ignores
// autodiscovery links whose href is not absolute.
func baseURL(r *http.Request) string { return "http://" + r.Host }

func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	c := s.live.Load()
	q := r.URL.Query().Get("q")
	if q == "" {
		onboarding(w, r, c)
		return
	}
	to := c.Resolve(q)
	if s.verbose {
		// RawQuery shows what the browser put on the wire, which is the point
		// of the flag: it settles how the omnibox encodes characters like #.
		log.Printf("q=%q raw=%q -> %s", q, r.URL.RawQuery, to)
	}
	// 302, not 301: browsers cache 301s permanently and a cached redirect would
	// survive every future config change.
	http.Redirect(w, r, to, http.StatusFound)
}

// handleResolve is a dry run: it reports where a query would go without
// redirecting, so rules can be checked from the onboarding page or curl
// without a browser navigating away.
func (s *server) handleResolve(w http.ResponseWriter, r *http.Request) {
	c := s.live.Load()
	q := r.URL.Query().Get("q")
	matched, rule := c.Match(q)
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(map[string]any{
		"query":   q,
		"target":  c.Resolve(q),
		"matched": matched,
		"rule":    rule,
	})
	if err != nil {
		log.Printf("resolve: %v", err)
	}
}
