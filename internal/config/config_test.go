package config_test

import (
	"os"
	"path/filepath"
	"slices"
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
		{
			// The raw escape is what keeps this a path rather than one long
			// percent-encoded segment.
			"f src/net/http/server.go",
			"https://github.com/golang/go/blob/master/src/net/http/server.go",
		},
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

func TestCaptureEscapeModes(t *testing.T) {
	// A path segment is the case a bare $1 cannot express: QueryEscape turns
	// "/" into %2F and a space into "+", both wrong outside a query string.
	c := configtest.Load(t, `
fallback: https://example.com/?q={{q}}
rules:
  - match: 'q (.+)'
    to: 'https://example.com/?s=$1'
  - match: 'p (.+)'
    to: 'https://example.com/${1:path}'
  - match: 'r (.+)'
    to: 'https://example.com/${1:raw}'
  - match: 'e (.+)'
    to: 'https://example.com/?s=${1:query}'
`)

	tests := []struct {
		name, in, want string
	}{
		{"query is the default", "q a b/c", "https://example.com/?s=a+b%2Fc"},
		{"query escapes separators", "q a&b=c", "https://example.com/?s=a%26b%3Dc"},
		{"path keeps spaces escaped", "p a b", "https://example.com/a%20b"},
		{"path escapes a separator", "p a/b", "https://example.com/a%2Fb"},
		{
			"raw passes a path through",
			"r golang/go",
			"https://example.com/golang/go",
		},
		{"query can be named", "e a&b", "https://example.com/?s=a%26b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Resolve(tt.in); got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSuggest(t *testing.T) {
	// Sample's only literal rule is "mr"; the rest are regexes whose literal
	// prefixes are "!", "p!", and "gh".
	c := configtest.Load(t, configtest.Sample)

	tests := []struct {
		name, yaml, q string
		limit         int
		want          []config.Suggestion
	}{
		{
			name:  "a literal rule offers its target",
			q:     "m",
			limit: 5,
			want: []config.Suggestion{{
				Query:  "mr",
				Desc:   configtest.SampleDesc,
				Target: configtest.MainMR,
			}},
		},
		{
			name:  "a regex rule offers only its prefix",
			q:     "p",
			limit: 5,
			want:  []config.Suggestion{{Query: "p!"}},
		},
		{name: "no rule starts with it", q: "zz", limit: 5},
		{name: "nothing is offered for an empty query", q: "", limit: 5},
		{name: "nothing is offered without a limit", q: "mr", limit: 0},
		{
			// The prefix repeats the literal rule below it, and the second "g1"
			// can never fire, so one entry is all the address bar should see.
			name: "an unreachable duplicate is skipped",
			yaml: `
fallback: https://example.com/?q={{q}}
rules:
  - match: 'g1(\d*)'
    to: 'https://example.com/n/$1'
  - match: 'g1'
    to: https://example.com/first
  - match: 'g1'
    to: https://example.com/second
`,
			q:     "g",
			limit: 5,
			want:  []config.Suggestion{{Query: "g1"}},
		},
		{
			// An earlier rule claims the literal, so the target offered has to
			// be the one the redirect would actually use.
			name: "a shadowed rule offers the winner's target",
			yaml: `
fallback: https://example.com/?q={{q}}
rules:
  - match: 'g.'
    to: https://example.com/regex
  - match: 'gh'
    to: https://example.com/literal
`,
			q:     "gh",
			limit: 5,
			want: []config.Suggestion{
				{Query: "gh", Target: "https://example.com/regex"},
			},
		},
		{
			name: "the limit stops the scan",
			yaml: `
fallback: https://example.com/?q={{q}}
rules:
  - match: 'g1'
    to: https://example.com/1
  - match: 'g2'
    to: https://example.com/2
  - match: 'g3'
    to: https://example.com/3
`,
			q:     "g",
			limit: 2,
			want: []config.Suggestion{
				{Query: "g1", Target: "https://example.com/1"},
				{Query: "g2", Target: "https://example.com/2"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := c
			if tt.yaml != "" {
				c = configtest.Load(t, tt.yaml)
			}
			got := c.Suggest(tt.q, tt.limit)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Suggest(%q, %d) = %+v, want %+v",
					tt.q, tt.limit, got, tt.want)
			}
		})
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
		{
			name: "annotated group out of range",
			yaml: head + `rules: [{match: 'a', to: 'https://x/${2:path}'}]`,
		},
		{
			name:    "unknown escape",
			yaml:    head + `rules: [{match: 'a (.+)', to: 'https://x/${1:shout}'}]`,
			wantErr: "unknown escape ${1:shout}",
		},
		// A malformed annotated capture matches no placeholder at all, so
		// without a check of its own it would reach the browser as literal text.
		{
			name:    "two-digit group",
			yaml:    head + `rules: [{match: 'a (.+)', to: 'https://x/${10:path}'}]`,
			wantErr: "malformed capture ${10:path}",
		},
		{
			name:    "unterminated capture",
			yaml:    head + `rules: [{match: 'a (.+)', to: 'https://x/${1:path'}]`,
			wantErr: "malformed capture ${1:path",
		},
		{
			name:    "escape left empty",
			yaml:    head + `rules: [{match: 'a (.+)', to: 'https://x/${1:}'}]`,
			wantErr: "malformed capture ${1:}",
		},
		{
			name:    "no escape given",
			yaml:    head + `rules: [{match: 'a (.+)', to: 'https://x/${1}'}]`,
			wantErr: "malformed capture ${1}",
		},
		{
			// Queries are trimmed before matching, so this rule could never
			// fire.
			name:    "match padded with space",
			yaml:    head + `rules: [{match: 'mr ', to: 'https://x/'}]`,
			wantErr: "padded with space",
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
