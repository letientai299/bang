package main

import (
	"encoding/json"
	"encoding/xml"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
)

// shortName must match the OpenSearch <ShortName> and the autodiscovery link's
// title attribute; Firefox rejects the descriptor if they differ. Max 16 chars.
const shortName = "bang"

type server struct {
	live    *atomic.Pointer[Config]
	verbose bool
}

func (s *server) routes() http.Handler {
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
		s.onboarding(w, r, c)
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

type openSearchDoc struct {
	XMLName       xml.Name      `xml:"OpenSearchDescription"`
	Xmlns         string        `xml:"xmlns,attr"`
	ShortName     string        `xml:"ShortName"`
	Description   string        `xml:"Description"`
	InputEncoding string        `xml:"InputEncoding"`
	URL           openSearchURL `xml:"Url"`
}

type openSearchURL struct {
	Type     string `xml:"type,attr"`
	Method   string `xml:"method,attr"`
	Template string `xml:"template,attr"`
}

func (s *server) handleOpenSearch(w http.ResponseWriter, r *http.Request) {
	// Marshalling rather than a text template: XML attribute escaping (notably
	// & in the search template) has to be exact or Firefox refuses the plugin.
	doc := openSearchDoc{
		Xmlns:         "http://a9.com/-/spec/opensearch/1.1/",
		ShortName:     shortName,
		Description:   "Personal address-bar shortcuts, resolved locally",
		InputEncoding: "UTF-8",
		URL: openSearchURL{
			Type:     "text/html",
			Method:   "get",
			Template: baseURL(r) + "/?q={searchTerms}",
		},
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/opensearchdescription+xml")
	if _, err := w.Write(append([]byte(xml.Header), out...)); err != nil {
		log.Printf("opensearch: %v", err)
	}
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

type pageData struct {
	*Config
	BaseURL   string
	ShortName string
	Browser   string
}

func (s *server) onboarding(w http.ResponseWriter, r *http.Request, c *Config) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := pageData{
		Config:    c,
		BaseURL:   baseURL(r),
		ShortName: shortName,
		Browser:   detectBrowser(r.UserAgent()),
	}
	if err := page.Execute(w, data); err != nil {
		log.Printf("onboarding: %v", err)
	}
}

// detectBrowser picks which set-up instructions to show first. Order matters:
// Chrome's UA contains "Safari", and Edge's contains "Chrome".
func detectBrowser(ua string) string {
	switch {
	case strings.Contains(ua, "Firefox"):
		return "firefox"
	case strings.Contains(ua, "Edg/"), strings.Contains(ua, "Chrome"):
		return "chrome"
	case strings.Contains(ua, "Safari"):
		return "safari"
	}
	return ""
}

var page = template.Must(template.New("").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>bang</title>
<!-- Must be on the site root with an absolute href, and ahead of any script,
     or Chrome ignores it. title must equal the descriptor's ShortName. -->
<link rel="search" type="application/opensearchdescription+xml"
      title="{{.ShortName}}" href="{{.BaseURL}}/opensearch.xml">
<style>
:root{color-scheme:light dark}
body{font:15px/1.65 ui-monospace,SFMono-Regular,Menlo,monospace;max-width:46em;margin:0 auto;padding:3em 1.5em}
h1{font-size:1.4em;margin:0}
h2{font-size:1em;margin:2.5em 0 .6em;padding-bottom:.3em;border-bottom:1px solid color-mix(in srgb,currentColor 20%,transparent)}
.sub{opacity:.65;margin:.3em 0 0}
code,input{font:inherit}
code{background:color-mix(in srgb,currentColor 10%,transparent);padding:.1em .35em;border-radius:3px}
table{border-collapse:collapse;width:100%}
td{padding:.35em 1.5em .35em 0;vertical-align:top}
td:first-child{white-space:nowrap}
ol{padding-left:1.3em}
li{margin:.4em 0}
.try{display:flex;gap:.5em;margin:.5em 0}
.try input{flex:1;padding:.5em .7em;border:1px solid color-mix(in srgb,currentColor 30%,transparent);border-radius:5px;background:transparent;color:inherit}
#out{margin:.5em 0 0;padding:.6em .8em;border-radius:5px;background:color-mix(in srgb,currentColor 8%,transparent);word-break:break-all;min-height:1.2em}
.muted{opacity:.6}
details{margin:.4em 0}
summary{cursor:pointer;opacity:.75}
</style>
</head>
<body>

<h1>bang</h1>
<p class="sub">Running at <code>{{.BaseURL}}</code> ·
{{if .Rules}}{{len .Rules}} rule{{if ne (len .Rules) 1}}s{{end}}{{else}}<strong>no rules loaded</strong> — fallback only{{end}}</p>

<h2>Try a shortcut</h2>
<div class="try">
  <input id="q" placeholder="!313" autofocus autocomplete="off" spellcheck="false">
</div>
<div id="out" class="muted">Type above to see where it would go. Nothing navigates.</div>

<h2>Set as your default search engine</h2>
{{if eq .Browser "firefox"}}
<ol>
  <li>Firefox has detected this page. Open the <strong>⋮⋮⋮ menu in the address bar</strong> (or right-click the address bar) and choose <strong>Add “{{.ShortName}}”</strong>.</li>
  <li>Settings → Search → <strong>Default Search Engine</strong> → pick <strong>{{.ShortName}}</strong>.</li>
</ol>
<details><summary>Autodiscovery not offered?</summary>
<p>Firefox 140+ can add it by hand: Settings → Search → Add search engine, URL
<code>{{.BaseURL}}/?q=%s</code>.</p></details>
{{else if eq .Browser "chrome"}}
<ol>
  <li>Chrome has already added this page to <em>Site search</em> automatically — but <strong>as inactive</strong>, which is why nothing works yet.</li>
  <li>Go to <code>chrome://settings/searchEngines</code>, find <strong>{{.ShortName}}</strong>, and click <strong>Activate</strong>.</li>
  <li>Open its ⋮ menu → <strong>Make default</strong>. This is the step that makes bare input like <code>!313</code> work.</li>
</ol>
<details><summary>Not listed?</summary>
<p>Add it manually with URL <code>{{.BaseURL}}/?q=%s</code>.</p></details>
{{else if eq .Browser "safari"}}
<p>Safari restricts the default search engine to a fixed list, so bang cannot be
installed here without a third-party extension that would see every query.
Use Chrome or Firefox.</p>
{{else}}
<p>Add a search engine with the URL <code>{{.BaseURL}}/?q=%s</code> and make it
your default.</p>
{{end}}

<h2>Rules</h2>
<table>
{{range .Rules}}<tr><td><code>{{.Match}}</code></td><td>{{.Desc}}</td></tr>
{{else}}<tr><td colspan="2" class="muted">None loaded.</td></tr>
{{end}}<tr><td class="muted">anything else</td><td class="muted">{{.Fallback}}</td></tr>
</table>

<script>
const q = document.getElementById("q"), out = document.getElementById("out");
let seq = 0;
q.addEventListener("input", async () => {
  const mine = ++seq, v = q.value;
  if (!v.trim()) { out.className = "muted"; out.textContent = "Type above to see where it would go. Nothing navigates."; return; }
  const r = await fetch("/resolve?q=" + encodeURIComponent(v)).then(r => r.json());
  if (mine !== seq) return; // a later keystroke already answered
  out.className = r.matched ? "" : "muted";
  out.textContent = (r.matched ? "→ " : "no rule, falls through → ") + r.target;
});
</script>
</body>
</html>
`))
