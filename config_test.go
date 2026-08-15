package main

import (
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, yaml string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return c
}

const testConfig = `
fallback: https://www.google.com/search?q={{q}}
vars:
  gl: https://gitlab.com
  repo: g/main
rules:
  - match: '!(\d+)'
    to: '{{gl}}/{{repo}}/-/merge_requests/$1'
  - match: 'p!(\d+)'
    to: '{{gl}}/g/platform/-/merge_requests/$1'
  - match: 'gh#?(\d+)'
    to: 'https://github.com/u/r/issues/$1'
  - match: 'mr'
    to: '{{gl}}/{{repo}}/-/merge_requests'
`

// Prefixes of the URLs testConfig resolves to, so the table below fits on one
// line per case and the interesting part of each expectation stays visible.
const (
	mainMR   = "https://gitlab.com/g/main/-/merge_requests"
	platform = "https://gitlab.com/g/platform/-/merge_requests"
	google   = "https://www.google.com/search?q="
)

func TestResolve(t *testing.T) {
	c := load(t, testConfig)

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

func TestFirstMatchWins(t *testing.T) {
	// "!(\d+)" would also match "p!313" if it were not anchored, so ordering
	// plus anchoring together must send it to the platform repo.
	c := load(t, testConfig)
	if got, want := c.Resolve("p!313"), platform+"/313"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCapturesAreEscaped(t *testing.T) {
	c := load(t, `
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

func TestSubstitutedTextIsNotRescanned(t *testing.T) {
	// A captured "{{gl}}" must survive as literal text, not expand into a var.
	c := load(t, `
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
		name string
		yaml string
	}{
		{"missing fallback", `rules: [{match: 'a', to: 'https://x'}]`},
		{"fallback without placeholder", `fallback: https://x/`},
		{"bad regex", head + `rules: [{match: '[', to: 'https://x'}]`},
		{"undefined var", head + `rules: [{match: 'a', to: '{{nope}}'}]`},
		{"group out of range", head + `rules: [{match: 'a', to: 'https://x/$2'}]`},
		{"unknown field", head + `rulez: []`},
		{"missing to", head + `rules: [{match: 'a'}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestSafeFallbackResolves(t *testing.T) {
	c := SafeFallback()
	if got, want := c.Resolve("hello world"), google+"hello+world"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
