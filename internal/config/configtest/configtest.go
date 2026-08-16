// Package configtest builds configs from literal YAML. Both the config and web
// tests need a loaded rule set, and Load is only reachable through a real file,
// so the temp-file dance lives here rather than in each test package.
package configtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/taile/bang/internal/config"
)

// Sample is the rule set the tests resolve against. It mirrors the shape of a
// real config — a bare sigil, a prefixed sigil, an optional separator, and a
// bare word — because those are the cases anchoring has to get right.
const Sample = `
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

// Targets that Sample resolves to, as prefixes, so test tables fit one case per
// line and the part that differs per row stays visible.
const (
	MainMR   = "https://gitlab.com/g/main/-/merge_requests"
	Platform = "https://gitlab.com/g/platform/-/merge_requests"
	Google   = "https://www.google.com/search?q="
)

// Load writes yaml to a temp file and loads it, failing the test if it does not
// parse.
func Load(t *testing.T, yaml string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return c
}
