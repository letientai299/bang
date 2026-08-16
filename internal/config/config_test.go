package config_test

import (
	"os"
	"path/filepath"
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

func TestFirstMatchWins(t *testing.T) {
	// "!(\d+)" would also match "p!313" if it were not anchored, so ordering
	// plus anchoring together must send it to the platform repo.
	c := configtest.Load(t, configtest.Sample)
	if got, want := c.Resolve("p!313"), platform+"/313"; got != want {
		t.Errorf("got %q, want %q", got, want)
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
			if _, err := config.Load(path); err == nil {
				t.Error("expected an error, got nil")
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
