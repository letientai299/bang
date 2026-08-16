package web

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/bang/internal/config"
	"github.com/example/bang/internal/config/configtest"
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
	mrTarget = configtest.MainMR + "/313"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	var live atomic.Pointer[config.Config]
	live.Store(configtest.Load(t, configtest.Sample))
	return Handler(&live, false)
}

func get(t *testing.T, path, ua string) *http.Response {
	t.Helper()
	return do(t, http.MethodGet, path, testHost, ua)
}

func do(t *testing.T, method, path, host, ua string) *http.Response {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	req.Host = host
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	w := httptest.NewRecorder()
	testHandler(t).ServeHTTP(w, req)
	return w.Result()
}

func TestOpenSearchDescriptor(t *testing.T) {
	resp := get(t, "/opensearch.xml", "")

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
	// Chrome takes its keyword from Alias, and lists the engine under the host
	// name when it is missing.
	if doc.Alias != shortName {
		t.Errorf("Alias = %q, want %q", doc.Alias, shortName)
	}

	byType := map[string]openSearchURL{}
	for _, u := range doc.URLs {
		byType[u.Type] = u
		if !strings.Contains(u.Template, "{searchTerms}") {
			t.Errorf("Url template %q lacks {searchTerms}", u.Template)
		}
		// Chrome ignores a descriptor whose template is not absolute.
		if !strings.HasPrefix(u.Template, testBase+"/") {
			t.Errorf("Url template %q is not absolute", u.Template)
		}
	}
	for _, want := range []string{"text/html", suggestionType} {
		if _, ok := byType[want]; !ok {
			t.Errorf("descriptor has no %s Url", want)
		}
	}
	// Chrome will not call a suggestions endpoint that shares the search URL.
	if byType[suggestionType].Template == byType["text/html"].Template {
		t.Errorf("suggestions and search share the template %q",
			byType["text/html"].Template)
	}
}

func TestSuggestions(t *testing.T) {
	// Sample has one literal rule, "mr", and three regex rules whose literal
	// prefixes are "!", "p!", and "gh".
	tests := []struct {
		name      string
		q, ua     string
		wantTexts []string
		wantDescs []string
		wantMeta  bool
		wantTypes []string
	}{
		{
			name:      "literal rule offers its target to chrome",
			q:         "m",
			ua:        chromeUA,
			wantTexts: []string{configtest.MainMR},
			// Chrome titles a navigation entry with the description.
			wantDescs: []string{configtest.SampleDesc},
			wantMeta:  true,
			wantTypes: []string{"NAVIGATION"},
		},
		{
			name: "firefox is offered the shortcut instead",
			// A URL sent back as a query would match no rule and land on the
			// fallback engine, so Firefox must never be given one.
			q:         "m",
			ua:        firefoxUA,
			wantTexts: []string{"mr"},
			wantDescs: []string{configtest.SampleDesc},
		},
		{
			name:      "a regex rule can only offer its prefix",
			q:         "p",
			ua:        chromeUA,
			wantTexts: []string{"p!"},
			// Sample's regex rules carry no desc, so the shortcut stands in.
			wantDescs: []string{"p!"},
			wantMeta:  true,
			wantTypes: []string{"QUERY"},
		},
		{
			name:      "an unknown browser gets the safe form",
			q:         "g",
			ua:        "curl/8.7.1",
			wantTexts: []string{"gh"},
			wantDescs: []string{"gh"},
		},
		{name: "no match suggests nothing", q: "zzz", ua: chromeUA, wantMeta: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := get(t, "/suggest?q="+url.QueryEscape(tt.q), tt.ua)
			if got := resp.Header.Get("Content-Type"); got != suggestionType {
				t.Errorf("Content-Type = %q, want %q", got, suggestionType)
			}

			var out []json.RawMessage
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}

			// Chrome discards the whole response when element 0 is not exactly
			// what was typed.
			var echoed string
			mustUnmarshal(t, out[0], &echoed)
			if echoed != tt.q {
				t.Errorf("echoed query = %q, want %q", echoed, tt.q)
			}

			var texts, descs []string
			mustUnmarshal(t, out[1], &texts)
			mustUnmarshal(t, out[2], &descs)
			if !slices.Equal(texts, tt.wantTexts) {
				t.Errorf("suggestions = %q, want %q", texts, tt.wantTexts)
			}
			if !slices.Equal(descs, tt.wantDescs) {
				t.Errorf("descriptions = %q, want %q", descs, tt.wantDescs)
			}

			if !tt.wantMeta {
				if len(out) > 4 {
					t.Errorf("metadata sent to a browser that cannot read it: %s",
						out[4])
				}
				return
			}
			var meta struct {
				Types []string `json:"google:suggesttype"`
			}
			mustUnmarshal(t, out[4], &meta)
			if !slices.Equal(meta.Types, tt.wantTypes) {
				t.Errorf("suggest types = %q, want %q", meta.Types, tt.wantTypes)
			}
		})
	}
}

func TestFaviconIsServed(t *testing.T) {
	resp := get(t, "/favicon.ico", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Chrome derives this path from the search URL and decodes by sniffing, so
	// PNG bytes under an .ico name are what it expects to find.
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if body := readAll(t, resp); !strings.HasPrefix(body, "\x89PNG") {
		t.Error("body is not a PNG")
	}
}

func mustUnmarshal(t *testing.T, raw json.RawMessage, into any) {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatal(err)
	}
}

func TestOnboardingAdvertisesDescriptor(t *testing.T) {
	body := readAll(t, get(t, "/", ""))

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
			body := readAll(t, get(t, "/", tt.ua))
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
	tests := []struct {
		name        string
		q           string
		wantMatched bool
		wantTarget  string
		wantRule    string
	}{
		{"match", "!313", true, mrTarget, `!(\d+)`},
		{
			"fallthrough",
			"hello world",
			false,
			configtest.Google + "hello+world",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := get(t, "/resolve?q="+url.QueryEscape(tt.q), "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200 (a dry run must not redirect)",
					resp.StatusCode)
			}
			var out struct {
				Target  string `json:"target"`
				Matched bool   `json:"matched"`
				Rule    string `json:"rule"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
			if out.Matched != tt.wantMatched || out.Target != tt.wantTarget ||
				out.Rule != tt.wantRule {
				t.Errorf(
					"got matched=%v target=%q rule=%q, want matched=%v target=%q rule=%q",
					out.Matched,
					out.Target,
					out.Rule,
					tt.wantMatched,
					tt.wantTarget,
					tt.wantRule,
				)
			}
		})
	}
}

func TestRootStillRedirectsWithQuery(t *testing.T) {
	resp := get(t, "/?q="+url.QueryEscape("!313"), "")
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != mrTarget {
		t.Errorf("Location = %q, want %q", got, mrTarget)
	}
	// Spelled out rather than compared against the constant: a redirect that
	// the browser is allowed to reuse would outlive the rule that produced it,
	// so this is the assertion that a future cache header has to argue with.
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
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
		{"suggestions", http.MethodGet, "/suggest", http.StatusOK},
		{"favicon", http.MethodGet, "/favicon.ico", http.StatusOK},
		{"no svg icon", http.MethodGet, "/favicon.svg", http.StatusNotFound},
		{"deep path", http.MethodGet, "/a/b", http.StatusNotFound},
		{"well-known probe", http.MethodGet, "/.well-known/x", http.StatusNotFound},
		{"post to root", http.MethodPost, "/", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := do(t, tt.method, tt.path, testHost, "")
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
			resp := do(t, http.MethodGet, "/", tt.host, "")
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
