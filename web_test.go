package main

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// Real user agents: Chrome's contains "Safari" and Edge's contains "Chrome",
// which is what makes the ordering in detectBrowser load-bearing.
const (
	chromeUA  = "Mozilla/5.0 (Macintosh) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36"
	edgeUA    = chromeUA + " Edg/140.0"
	firefoxUA = "Mozilla/5.0 (Macintosh; rv:141.0) Gecko/20100101 Firefox/141.0"
	safariUA  = "Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
)

const (
	testHost = "127.0.0.1:8111"
	testBase = "http://" + testHost
	mrTarget = "https://gitlab.com/g/main/-/merge_requests/313"
)

func testServer(t *testing.T) *server {
	t.Helper()
	var live atomic.Pointer[Config]
	live.Store(load(t, testConfig))
	return &server{live: &live}
}

func get(t *testing.T, s *server, path, ua string) *http.Response {
	t.Helper()
	return do(t, s, http.MethodGet, path, testHost, ua)
}

func do(t *testing.T, s *server, method, path, host, ua string) *http.Response {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	req.Host = host
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	return w.Result()
}

func TestOpenSearchDescriptor(t *testing.T) {
	resp := get(t, testServer(t), "/opensearch.xml", "")

	// Firefox refuses the plugin outright without this exact content type.
	const wantType = "application/opensearchdescription+xml"
	if got := resp.Header.Get("Content-Type"); got != wantType {
		t.Errorf("Content-Type = %q, want %q", got, wantType)
	}

	var doc openSearchDoc
	if err := xml.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("descriptor is not well-formed XML: %v", err)
	}

	if doc.Xmlns != "http://a9.com/-/spec/opensearch/1.1/" {
		t.Errorf("xmlns = %q; Firefox rejects the plugin without it", doc.Xmlns)
	}
	if doc.ShortName != shortName {
		t.Errorf("ShortName = %q, want %q (must match the link title)",
			doc.ShortName, shortName)
	}
	if len(doc.ShortName) > 16 {
		t.Errorf("ShortName %q exceeds the 16 character limit", doc.ShortName)
	}
	if doc.Description == "" || doc.InputEncoding == "" {
		t.Error("Description and InputEncoding are both required")
	}
	if doc.URL.Type != "text/html" {
		t.Errorf("Url type = %q; Firefox needs text/html", doc.URL.Type)
	}
	if !strings.Contains(doc.URL.Template, "{searchTerms}") {
		t.Errorf("Url template %q lacks {searchTerms}", doc.URL.Template)
	}
	// Chrome ignores a descriptor whose template is not absolute.
	if !strings.HasPrefix(doc.URL.Template, testBase+"/") {
		t.Errorf("Url template %q is not absolute", doc.URL.Template)
	}
}

func TestOnboardingAdvertisesDescriptor(t *testing.T) {
	body := readAll(t, get(t, testServer(t), "/", ""))

	// The link must carry an absolute href and a title equal to ShortName, or
	// Chrome ignores it and Firefox rejects it respectively.
	for _, want := range []string{
		`rel="search"`,
		`type="application/opensearchdescription+xml"`,
		`title="` + shortName + `"`,
		`href="` + testBase + `/opensearch.xml"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("onboarding page is missing %s", want)
		}
	}

	// Chrome stops looking for the link once a script tag appears.
	if strings.Index(body, `rel="search"`) > strings.Index(body, "<script") {
		t.Error(`rel="search" appears after <script>; Chrome will ignore it`)
	}
}

func TestOnboardingIsBrowserSpecific(t *testing.T) {
	tests := []struct {
		name, ua, want string
	}{
		{"chrome is told to activate", chromeUA, "Activate"},
		{"firefox uses autodiscovery", firefoxUA, "Add “" + shortName + "”"},
		{"safari is told it cannot work", safariUA, "fixed list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := readAll(t, get(t, testServer(t), "/", tt.ua))
			if !strings.Contains(body, tt.want) {
				t.Errorf("page does not mention %q", tt.want)
			}
		})
	}
}

func TestDetectBrowser(t *testing.T) {
	tests := []struct{ name, ua, want string }{
		{"firefox", firefoxUA, "firefox"},
		{"chrome", chromeUA, "chrome"},
		{"edge is not chrome-by-accident", edgeUA, "chrome"},
		{"safari is not chrome", safariUA, "safari"},
		{"unknown", "curl/8.7.1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectBrowser(tt.ua); got != tt.want {
				t.Errorf("detectBrowser = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveEndpointDoesNotRedirect(t *testing.T) {
	const googleTarget = "https://www.google.com/search?q=hello+world"

	tests := []struct {
		name        string
		q           string
		wantMatched bool
		wantTarget  string
	}{
		{"match", "!313", true, mrTarget},
		{"fallthrough", "hello world", false, googleTarget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/resolve?q=" + url.QueryEscape(tt.q)
			resp := get(t, testServer(t), path, "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200 (a dry run must not redirect)",
					resp.StatusCode)
			}
			var out struct {
				Target  string `json:"target"`
				Matched bool   `json:"matched"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
			if out.Matched != tt.wantMatched || out.Target != tt.wantTarget {
				t.Errorf("got matched=%v target=%q, want matched=%v target=%q",
					out.Matched, out.Target, tt.wantMatched, tt.wantTarget)
			}
		})
	}
}

func TestRootStillRedirectsWithQuery(t *testing.T) {
	resp := get(t, testServer(t), "/?q="+url.QueryEscape("!313"), "")
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != mrTarget {
		t.Errorf("Location = %q, want %q", got, mrTarget)
	}
}

// Stray paths must 404 rather than render the onboarding page: a bare "/"
// pattern is a catch-all, so a 200 on /favicon.ico told the browser the icon
// existed and was an HTML document.
func TestOnlyKnownRoutesAreServed(t *testing.T) {
	tests := []struct {
		name, method, path string
		want               int
	}{
		{"root", http.MethodGet, "/", http.StatusOK},
		{"descriptor", http.MethodGet, "/opensearch.xml", http.StatusOK},
		{"dry run", http.MethodGet, "/resolve", http.StatusOK},
		{"favicon", http.MethodGet, "/favicon.ico", http.StatusNotFound},
		{"deep path", http.MethodGet, "/a/b", http.StatusNotFound},
		{"post to root", http.MethodPost, "/", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testServer(t)
			resp := do(t, s, tt.method, tt.path, testHost, "")
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}

// A DNS-rebinding page reaches the loopback listener but cannot forge Host.
func TestNonLoopbackHostIsRejected(t *testing.T) {
	tests := []struct {
		name, host string
		want       int
	}{
		{"ipv4 loopback", testHost, http.StatusOK},
		{"ipv4 loopback subnet", "127.9.9.9:8111", http.StatusOK},
		{"ipv4 loopback no port", "127.0.0.1", http.StatusOK},
		{"localhost", "localhost:8111", http.StatusOK},
		{"localhost is case-insensitive", "LocalHost:8111", http.StatusOK},
		{"ipv6 loopback", "[::1]:8111", http.StatusOK},
		{"ipv6 loopback no port", "[::1]", http.StatusOK},
		{"rebound hostname", "evil.example.com:8111", http.StatusForbidden},
		{"lan address", "192.168.1.4:8111", http.StatusForbidden},
		{"empty", "", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testServer(t)
			resp := do(t, s, http.MethodGet, "/", tt.host, "")
			if resp.StatusCode != tt.want {
				t.Errorf("Host %q: status = %d, want %d",
					tt.host, resp.StatusCode, tt.want)
			}
		})
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
