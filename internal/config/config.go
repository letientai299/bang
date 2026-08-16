// Package config parses the on-disk rule set and resolves address-bar queries
// against it. It is the half of bang that decides where a query goes; see
// internal/web for the half that speaks HTTP.
package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk rule set. It is reloaded on every write to the file; a
// config that fails to parse is rejected and the previous one stays live, so a
// typo can never take down the address bar.
type Config struct {
	Listen   string            `yaml:"listen"`
	Fallback string            `yaml:"fallback"`
	Vars     map[string]string `yaml:"vars"`
	Rules    []Rule            `yaml:"rules"`

	literalRules map[string]int
	regexRules   []int
}

// Rule maps one address-bar pattern to one destination. Match is compiled
// anchored, so it claims a query only when it matches the whole of it.
type Rule struct {
	Match string `yaml:"match"`
	To    string `yaml:"to"`
	Desc  string `yaml:"desc"`

	re         *regexp.Regexp
	prefix     string
	target     string
	targetSize int
	parts      []targetPart
}

type targetPart struct {
	text    string
	capture int
}

// SafeFallback is served when no config has ever loaded successfully. Without
// it, a bad config on a cold start would leave the browser with a dead search
// bar rather than a degraded one.
func SafeFallback() *Config {
	return &Config{
		Listen:   "127.0.0.1:8111",
		Fallback: "https://www.google.com/search?q={{q}}",
	}
}

// placeholder matches both capture-group refs ($1..$9) and var refs ({{name}}).
// Both are handled in a single pass so that substituted text is never rescanned
// — a captured query containing "{{gl}}" must not expand into a var.
var placeholder = regexp.MustCompile(`\$([1-9])|\{\{(\w+)\}\}`)

// varPlaceholder is separate from placeholder because captures have no meaning
// inside vars. Vars are fully resolved when the config loads, before a rule
// expands its request-time captures.
var varPlaceholder = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Load reads and validates the rule set at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}

	c.Listen = cmp.Or(c.Listen, SafeFallback().Listen)
	if !strings.Contains(c.Fallback, "{{q}}") {
		return nil, errors.New("fallback must be set and contain {{q}}")
	}
	if err := c.resolveVars(); err != nil {
		return nil, err
	}

	c.literalRules = make(map[string]int, len(c.Rules))
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.Match == "" || r.To == "" {
			return nil, fmt.Errorf("rule %d: both match and to are required", i+1)
		}
		// Compile the pattern as written first, so a syntax error reports what
		// the user typed rather than the anchors added below.
		rawRE, err := regexp.Compile(r.Match)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", i+1, err)
		}
		r.prefix, _ = rawRE.LiteralPrefix()
		// Anchored implicitly: a rule that matched a substring of an ordinary
		// search query would silently hijack real searches.
		re, err := regexp.Compile(`\A(?:` + r.Match + `)\z`)
		if err != nil {
			return nil, fmt.Errorf("rule %d (%s): %w", i+1, r.Match, err)
		}
		r.re = re

		if err := c.validate(r); err != nil {
			return nil, fmt.Errorf("rule %d (%s): %w", i+1, r.Match, err)
		}
		c.compileTarget(r)
		if regexp.QuoteMeta(r.Match) == r.Match {
			if _, exists := c.literalRules[r.Match]; !exists {
				c.literalRules[r.Match] = i
			}
		} else {
			c.regexRules = append(c.regexRules, i)
		}
	}
	return &c, nil
}

// compileTarget resolves static vars and splits capture substitutions once at
// load time. Resolve can then assemble a target without reparsing its template.
func (c *Config) compileTarget(r *Rule) {
	parts := make([]targetPart, 0, 4)
	appendText := func(text string) {
		if text == "" {
			return
		}
		r.targetSize += len(text)
		if len(parts) > 0 && parts[len(parts)-1].capture == 0 {
			parts[len(parts)-1].text += text
			return
		}
		parts = append(parts, targetPart{text: text})
	}

	last := 0
	hasCapture := false
	for _, loc := range placeholder.FindAllStringSubmatchIndex(r.To, -1) {
		appendText(r.To[last:loc[0]])
		switch {
		case loc[2] >= 0:
			hasCapture = true
			parts = append(parts, targetPart{capture: int(r.To[loc[2]] - '0')})
		default:
			appendText(c.Vars[r.To[loc[4]:loc[5]]])
		}
		last = loc[1]
	}
	appendText(r.To[last:])

	if hasCapture {
		r.parts = parts
		return
	}
	if len(parts) > 0 {
		r.target = parts[0].text
	}
}

// resolveVars expands references between vars. A depth-first traversal permits
// forward references while detecting cycles before any partially expanded value
// reaches a rule.
func (c *Config) resolveVars() error {
	const (
		visiting = 1
		done     = 2
	)

	raw := c.Vars
	resolved := make(map[string]string, len(raw))
	state := make(map[string]int, len(raw))
	stack := make([]string, 0, len(raw))

	var resolve func(string) (string, error)
	resolve = func(name string) (string, error) {
		switch state[name] {
		case done:
			return resolved[name], nil
		case visiting:
			start := slices.Index(stack, name)
			cycle := append(slices.Clone(stack[start:]), name)
			for i := range cycle {
				cycle[i] = "{{" + cycle[i] + "}}"
			}
			return "", fmt.Errorf("variable cycle: %s", strings.Join(cycle, " -> "))
		}

		value, ok := raw[name]
		if !ok {
			return "", fmt.Errorf("undefined var {{%s}}", name)
		}

		state[name] = visiting
		stack = append(stack, name)
		var expandErr error
		value = varPlaceholder.ReplaceAllStringFunc(value, func(ref string) string {
			if expandErr != nil {
				return ref
			}
			match := varPlaceholder.FindStringSubmatch(ref)
			expanded, err := resolve(match[1])
			if err != nil {
				expandErr = err
				return ref
			}
			return expanded
		})
		stack = stack[:len(stack)-1]
		if expandErr != nil {
			return "", expandErr
		}

		state[name] = done
		resolved[name] = value
		return value, nil
	}

	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if _, err := resolve(name); err != nil {
			return fmt.Errorf("var {{%s}}: %w", name, err)
		}
	}
	c.Vars = resolved
	return nil
}

// validate rejects references that could not possibly resolve at request time,
// so mistakes surface on save rather than on a redirect to a broken URL.
func (c *Config) validate(r *Rule) error {
	for _, m := range placeholder.FindAllStringSubmatch(r.To, -1) {
		switch {
		case m[1] != "":
			n, _ := strconv.Atoi(m[1])
			if groups := r.re.NumSubexp(); n > groups {
				return fmt.Errorf("$%d but pattern has %d capture groups", n, groups)
			}
		case m[2] == "q":
			return errors.New("{{q}} is only valid in fallback; use a capture group")
		default:
			if _, ok := c.Vars[m[2]]; !ok {
				return fmt.Errorf("undefined var {{%s}}", m[2])
			}
		}
	}
	return nil
}

// ResolveMatch returns the target plus match metadata in one rule scan.
func (c *Config) ResolveMatch(q string) (
	target string,
	matched bool,
	pattern string,
) {
	q = strings.TrimSpace(q)
	r, groups := c.find(q, true)
	if r != nil {
		return expandTarget(r, q, groups), true, r.Match
	}
	return strings.ReplaceAll(c.Fallback, "{{q}}", url.QueryEscape(q)), false, ""
}

// Resolve returns the URL for a raw address-bar query. It always returns a
// usable URL: an unmatched query falls through to the configured search engine.
func (c *Config) Resolve(q string) string {
	target, _, _ := c.ResolveMatch(q)
	return target
}

// Match reports whether a rule claimed the query, and which pattern did. It
// exists so the dry-run endpoint can distinguish a real hit from a fallback,
// which Resolve alone cannot express.
func (c *Config) Match(q string) (matched bool, pattern string) {
	q = strings.TrimSpace(q)
	r, _ := c.find(q, false)
	if r == nil {
		return false, ""
	}
	return true, r.Match
}

// find checks only regex rules that could precede an exact match. This keeps
// first-match-wins semantics while letting literal-only configs avoid regexes.
func (c *Config) find(q string, captures bool) (rule *Rule, groups []int) {
	literalIndex, hasLiteral := c.literalRules[q]
	for _, i := range c.regexRules {
		if hasLiteral && i > literalIndex {
			break
		}
		r := &c.Rules[i]
		if r.prefix != "" && !strings.HasPrefix(q, r.prefix) {
			continue
		}
		if captures && len(r.parts) > 0 {
			groups := r.re.FindStringSubmatchIndex(q)
			if groups != nil {
				return r, groups
			}
			continue
		}
		if r.re.MatchString(q) {
			return r, nil
		}
	}
	if hasLiteral {
		return &c.Rules[literalIndex], nil
	}
	return nil, nil
}

func expandTarget(r *Rule, q string, groups []int) string {
	if len(r.parts) == 0 {
		return r.target
	}
	var target strings.Builder
	target.Grow(r.targetSize + len(q))
	for _, part := range r.parts {
		if part.capture == 0 {
			target.WriteString(part.text)
			continue
		}
		start, end := groups[part.capture*2], groups[part.capture*2+1]
		if start >= 0 {
			// Captures come from the address bar, so they are escaped. Rules
			// that need a raw path segment should hard-code it or use a var.
			target.WriteString(url.QueryEscape(q[start:end]))
		}
	}
	return target.String()
}
