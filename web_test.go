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

func testServer(t *testing.T) *server {
	t.Helper()
	var live atomic.Pointer[Config]
	live.Store(load(t, testConfig))
	return &server{live: &live}
}

func get(t *testing.T, s *server, path, ua string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:8111"
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
	if got := resp.Header.Get("Content-Type"); got != "application/opensearchdescription+xml" {
		t.Errorf("Content-Type = %q", got)
	}

	var doc openSearchDoc
	if err := xml.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("descriptor is not well-formed XML: %v", err)
	}

	if doc.Xmlns != "http://a9.com/-/spec/opensearch/1.1/" {
		t.Errorf("xmlns = %q; omitting it makes Firefox reject the plugin", doc.Xmlns)
	}
	if doc.ShortName != shortName {
		t.Errorf("ShortName = %q, want %q (must match the link title)", doc.ShortName, shortName)
	}
	if len(doc.ShortName) > 16 {
		t.Errorf("ShortName %q exceeds the 16 character limit", doc.ShortName)
	}
	if doc.Description == "" || doc.InputEncoding == "" {
		t.Error("Description and InputEncoding are both required")
	}
	if doc.URL.Type != "text/html" {
		t.Errorf("Url type = %q; Firefox errors without a text/html Url", doc.URL.Type)
	}
	if !strings.Contains(doc.URL.Template, "{searchTerms}") {
		t.Errorf("Url template %q lacks {searchTerms}", doc.URL.Template)
	}
	// Chrome ignores a descriptor whose template is not absolute.
	if !strings.HasPrefix(doc.URL.Template, "http://127.0.0.1:8111/") {
		t.Errorf("Url template %q is not absolute against the request host", doc.URL.Template)
	}
}

func TestOnboardingAdvertisesDescriptor(t *testing.T) {
	resp := get(t, testServer(t), "/", "")
	body := readAll(t, resp)

	// The link must carry an absolute href and a title equal to ShortName, or
	// Chrome ignores it and Firefox rejects it respectively.
	for _, want := range []string{
		`rel="search"`,
		`type="application/opensearchdescription+xml"`,
		`title="` + shortName + `"`,
		`href="http://127.0.0.1:8111/opensearch.xml"`,
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
	const (
		chromeUA  = "Mozilla/5.0 (Macintosh) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36"
		firefoxUA = "Mozilla/5.0 (Macintosh; rv:141.0) Gecko/20100101 Firefox/141.0"
		safariUA  = "Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
	)

	tests := []struct {
		name, ua, want string
	}{
		{"chrome is told to activate", chromeUA, "Activate"},
		{"firefox is told to use autodiscovery", firefoxUA, "Add “" + shortName + "”"},
		{"safari is told it cannot work", safariUA, "fixed list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if body := readAll(t, get(t, testServer(t), "/", tt.ua)); !strings.Contains(body, tt.want) {
				t.Errorf("page for %s does not mention %q", tt.name, tt.want)
			}
		})
	}
}

func TestDetectBrowser(t *testing.T) {
	tests := []struct{ name, ua, want string }{
		{"firefox", "Mozilla/5.0 (Macintosh; rv:141.0) Gecko/20100101 Firefox/141.0", "firefox"},
		{"chrome", "Mozilla/5.0 AppleWebKit/537.36 Chrome/140.0 Safari/537.36", "chrome"},
		// Edge's UA contains "Chrome", and Chrome's contains "Safari", so the
		// ordering of the checks is load-bearing.
		{"edge is not chrome-by-accident", "Mozilla/5.0 AppleWebKit/537.36 Chrome/140.0 Safari/537.36 Edg/140.0", "chrome"},
		{"safari is not chrome", "Mozilla/5.0 AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15", "safari"},
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
	tests := []struct {
		name        string
		q           string
		wantMatched bool
		wantTarget  string
	}{
		{"match", "!313", true, "https://gitlab.com/g/main/-/merge_requests/313"},
		{"fallthrough", "hello world", false, "https://www.google.com/search?q=hello+world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := get(t, testServer(t), "/resolve?q="+url.QueryEscape(tt.q), "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200 (a dry run must not redirect)", resp.StatusCode)
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
	if got := resp.Header.Get("Location"); got != "https://gitlab.com/g/main/-/merge_requests/313" {
		t.Errorf("Location = %q", got)
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
