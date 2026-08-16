// Package configtest builds configs from literal TOML. Both the config and web
// tests need a loaded rule set, and Load is only reachable through a real file,
// so the temp-file dance lives here rather than in each test package.
package configtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/example/bang/internal/config"
)

// Sample is the rule set the tests resolve against. It mirrors the shape of a
// real config — a bare sigil, a prefixed sigil, an optional separator, and a
// bare word — because those are the cases anchoring has to get right.
const Sample = `
fallback = 'https://www.google.com/search?q={{q}}'
rules = [
  ['!(\d+)', '{{gl}}/{{repo}}/-/merge_requests/$1'],
  ['p!(\d+)', '{{gl}}/g/platform/-/merge_requests/$1'],
  ['gh#?(\d+)', 'https://github.com/u/r/issues/$1'],
  ['mr', '{{gl}}/{{repo}}/-/merge_requests', 'Open merge requests'],
]

[vars]
gl = 'https://gitlab.com'
repo = 'g/main'
`

// SampleDesc is the desc of Sample's "mr" rule. Only one rule carries one, so
// the tests can check both what a description does and what stands in for a
// missing one.
const SampleDesc = "Open merge requests"

// Targets that Sample resolves to, as prefixes, so test tables fit one case per
// line and the part that differs per row stays visible.
const (
	MainMR   = "https://gitlab.com/g/main/-/merge_requests"
	Platform = "https://gitlab.com/g/platform/-/merge_requests"
	Google   = "https://www.google.com/search?q="
)

// Load writes TOML to a temp file and loads it, failing the test if it does not
// parse.
func Load(t *testing.T, tomlText string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(tomlText), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return c
}
