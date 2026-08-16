package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/bang/internal/config"
	"github.com/example/bang/internal/config/configtest"
)

const (
	mainMR   = configtest.MainMR
	platform = configtest.Platform
	google   = configtest.Google
)

func TestResolve(t *testing.T) {
	c := configtest.Load(t, configtest.Sample)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare sigil", "!313", mainMR + "/313"},
		{"prefixed sigil", "p!313", platform + "/313"},
		{"hash form", "gh#12", "https://github.com/u/r/issues/12"},
		{"hashless form", "gh12", "https://github.com/u/r/issues/12"},
		{"bare word", "mr", mainMR},
		{"surrounding space", "  !313  ", mainMR + "/313"},

		// Rules must not fire on anything that merely contains them, or normal
		// searching breaks.
		{"substring", "why is mr robot good", google + "why+is+mr+robot+good"},
		{"prefix is not a match", "mri scan", google + "mri+scan"},
		{"sigil inside a phrase", "wow !313 huh", google + "wow+%21313+huh"},
		{"unmatched falls through", "golang defer", google + "golang+defer"},
		{"empty query", "", google},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Resolve(tt.in); got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExampleConfig(t *testing.T) {
	c, err := config.Load(filepath.Join("..", "..", "deploy", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		query string
		want  string
	}{
		{"pr", "https://github.com/golang/go/pulls"},
		{"#123", "https://github.com/golang/go/issues/123"},
		{"is", "https://github.com/golang/go/issues"},
		{"m", "https://music.youtube.com/"},
		{"hn", "https://news.ycombinator.com/"},
		{"m jazz fusion", "https://music.youtube.com/search?q=jazz+fusion"},
		{
			"y Go concurrency",
			"https://www.youtube.com/results?search_query=Go+concurrency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := c.Resolve(tt.query); got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestFirstMatchWins(t *testing.T) {
	// "!(\d+)" would also match "p!313" if it were not anchored, so ordering
	// plus anchoring together must send it to the platform repo.
	c := configtest.Load(t, configtest.Sample)
	if got, want := c.Resolve("p!313"), platform+"/313"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLiteralIndexPreservesRuleOrder(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "earlier regex wins",
			yaml: `
fallback: https://example.com/?q={{q}}
rules:
  - match: 'p.*'
    to: https://example.com/regex
  - match: 'pr'
    to: https://example.com/literal
`,
			want: "https://example.com/regex",
		},
		{
			name: "earlier literal wins",
			yaml: `
fallback: https://example.com/?q={{q}}
rules:
  - match: 'pr'
    to: https://example.com/literal
  - match: 'p.*'
    to: https://example.com/regex
`,
			want: "https://example.com/literal",
		},
		{
			name: "first duplicate literal wins",
			yaml: `
fallback: https://example.com/?q={{q}}
rules:
  - match: 'pr'
    to: https://example.com/first
  - match: 'pr'
    to: https://example.com/second
`,
			want: "https://example.com/first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := configtest.Load(t, tt.yaml)
			if got := c.Resolve("pr"); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCapturesAreEscaped(t *testing.T) {
	c := configtest.Load(t, `
fallback: https://www.google.com/search?q={{q}}
rules:
  - match: 'x (.+)'
    to: 'https://example.com/?s=$1'
`)
	got, want := c.Resolve("x a&b=c"), "https://example.com/?s=a%26b%3Dc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOptionalCaptureCanBeEmpty(t *testing.T) {
	c := configtest.Load(t, `
fallback: https://example.com/?q={{q}}
rules:
  - match: 'x(?: (.+))?'
    to: 'https://example.com/?q=$1'
`)
	if got, want := c.Resolve("x"), "https://example.com/?q="; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestVarsCanReferToVars(t *testing.T) {
	c := configtest.Load(t, `
fallback: https://www.google.com/search?q={{q}}
vars:
  gh: https://github.com
  owner: golang
  org: "{{gh}}/{{owner}}"
  repo: "{{org}}/go"
rules:
  - match: '#(\d+)'
    to: '{{repo}}/issues/$1'
`)

	got := c.Resolve("#123")
	want := "https://github.com/golang/go/issues/123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := c.Vars["repo"], "https://github.com/golang/go"; got != want {
		t.Errorf("resolved repo var = %q, want %q", got, want)
	}
}

func TestSubstitutedTextIsNotRescanned(t *testing.T) {
	// A captured "{{gl}}" must survive as literal text, not expand into a var.
	c := configtest.Load(t, `
fallback: https://www.google.com/search?q={{q}}
vars:
  gl: https://gitlab.com
rules:
  - match: 'x (.+)'
    to: 'https://example.com/?s=$1'
`)
	got, want := c.Resolve("x {{gl}}"), "https://example.com/?s=%7B%7Bgl%7D%7D"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBadConfigIsRejected(t *testing.T) {
	const head = "fallback: 'https://x?q={{q}}'\n"

	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "missing fallback", yaml: `rules: [{match: 'a', to: 'https://x'}]`},
		{name: "fallback without placeholder", yaml: `fallback: https://x/`},
		{name: "bad regex", yaml: head + `rules: [{match: '[', to: 'https://x'}]`},
		{
			name: "undefined rule var",
			yaml: head + `rules: [{match: 'a', to: '{{nope}}'}]`,
		},
		{
			name: "group out of range",
			yaml: head + `rules: [{match: 'a', to: 'https://x/$2'}]`,
		},
		{name: "unknown field", yaml: head + `rulez: []`},
		{name: "missing to", yaml: head + `rules: [{match: 'a'}]`},
		{
			name:    "undefined nested var",
			yaml:    head + `vars: {repo: '{{missing}}/repo'}`,
			wantErr: "var {{repo}}: undefined var {{missing}}",
		},
		{
			name:    "direct variable cycle",
			yaml:    head + `vars: {repo: '{{repo}}'}`,
			wantErr: "variable cycle: {{repo}} -> {{repo}}",
		},
		{
			name: "indirect variable cycle",
			yaml: head + `vars:
  org: '{{repo}}'
  repo: '{{org}}'
`,
			wantErr: "variable cycle: {{org}} -> {{repo}} -> {{org}}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := config.Load(path)
			if err == nil {
				t.Error("expected an error, got nil")
			} else if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestSafeFallbackResolves(t *testing.T) {
	c := config.SafeFallback()
	if got, want := c.Resolve("hello world"), google+"hello+world"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
