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
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the on-disk rule set. It is reloaded on every write to the file; a
// config that fails to parse is rejected and the previous one stays live, so a
// typo can never take down the address bar.
type Config struct {
	Listen   string
	Fallback string
	Vars     map[string]string
	Rules    []Rule

	literalRules map[string]int
	regexRules   []int
}

// Rule maps one address-bar pattern to one destination. Match is compiled
// anchored, so it claims a query only when it matches the whole of it.
type Rule struct {
	Match string
	To    string
	Desc  string

	// Expanded is To with vars substituted and capture refs left as they were
	// written. It is what a rule has to show a reader who is deciding whether
	// it is the one they want: To alone hides the destination behind a var
	// name, and the resolved target only exists once a query has been matched.
	// It is filled in at load time and never read from the config file.
	Expanded string

	re         *regexp.Regexp
	prefix     string
	target     string
	targetSize int
	parts      []targetPart
}

// diskConfig is the deliberately small TOML surface. Rules are positional so
// the common case stays on one line: [match, target], with an optional third
// description. Load normalizes them into Rule before validation and use.
type diskConfig struct {
	Listen   string            `toml:"listen"`
	Fallback string            `toml:"fallback"`
	Vars     map[string]string `toml:"vars"`
	Rules    [][]string        `toml:"rules"`
}

type targetPart struct {
	text    string
	capture int
	escape  func(string) string
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

// placeholder matches capture-group refs — plain ($1..$9) or annotated with an
// escape (${1:path}) — and var refs ({{name}}). All are handled in a single
// pass so that substituted text is never rescanned: a captured query containing
// "{{gl}}" must not expand into a var.
var placeholder = regexp.MustCompile(
	`\$\{([1-9]):(\w+)\}|\$([1-9])|\{\{(\w+)\}\}`,
)

// defaultEscape is what a bare $1 means. It is right for a search parameter
// and wrong for a path segment: QueryEscape turns "/" into %2F and a space
// into "+", which is why the annotated form exists.
const defaultEscape = "query"

// escapes are the substitution modes a capture can be annotated with.
var escapes = map[string]func(string) string{
	"query": url.QueryEscape,
	"path":  url.PathEscape,
	"raw":   func(s string) string { return s },
}

// ref is one decoded placeholder: either a capture with the escape it asked
// for, or the name of a var.
type ref struct {
	capture int
	escape  string
	name    string
}

// decodeRef reads one placeholder match. Validation and target compilation both
// go through it, so which submatch means what is stated once.
func decodeRef(to string, loc []int) ref {
	group := func(n int) string {
		if loc[2*n] < 0 {
			return ""
		}
		return to[loc[2*n]:loc[2*n+1]]
	}
	switch {
	case group(1) != "":
		return ref{capture: int(group(1)[0] - '0'), escape: group(2)}
	case group(3) != "":
		return ref{capture: int(group(3)[0] - '0'), escape: defaultEscape}
	default:
		return ref{name: group(4)}
	}
}

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

	var disk diskConfig
	dec := toml.NewDecoder(bytes.NewReader(raw)).DisallowUnknownFields()
	if err := dec.Decode(&disk); err != nil {
		return nil, err
	}

	c := Config{
		Listen:   disk.Listen,
		Fallback: disk.Fallback,
		Vars:     disk.Vars,
		Rules:    make([]Rule, len(disk.Rules)),
	}
	for i, tuple := range disk.Rules {
		if len(tuple) < 2 || len(tuple) > 3 {
			return nil, fmt.Errorf(
				"rule %d: want [match, target] or [match, target, description]",
				i+1,
			)
		}
		c.Rules[i] = Rule{Match: tuple[0], To: tuple[1]}
		if len(tuple) == 3 {
			c.Rules[i].Desc = tuple[2]
		}
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
			return nil, fmt.Errorf("rule %d: both match and target are required", i+1)
		}
		// A query is trimmed before it is matched, so a pattern padded with
		// space can never fire. Saying so beats leaving a rule that silently
		// does nothing.
		if strings.TrimSpace(r.Match) != r.Match {
			return nil, fmt.Errorf(
				"rule %d (%s): match is padded with space, which no query can carry",
				i+1, r.Match,
			)
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
	var expanded strings.Builder
	parts := make([]targetPart, 0, 4)
	appendText := func(text string) {
		if text == "" {
			return
		}
		expanded.WriteString(text)
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
		switch ref := decodeRef(r.To, loc); {
		case ref.capture > 0:
			hasCapture = true
			// A capture has no value until a query arrives, so the expanded
			// form carries the ref as typed — including its escape, which is
			// part of what the rule does.
			expanded.WriteString(r.To[loc[0]:loc[1]])
			parts = append(parts, targetPart{
				capture: ref.capture,
				escape:  escapes[ref.escape],
			})
		default:
			appendText(c.Vars[ref.name])
		}
		last = loc[1]
	}
	appendText(r.To[last:])
	r.Expanded = expanded.String()

	if hasCapture {
		r.parts = parts
		return
	}
	// With no captures every part is text, and appendText merged them all into
	// the first one, which makes it the whole expanded target.
	r.target = r.Expanded
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
	last := 0
	for _, loc := range placeholder.FindAllStringSubmatchIndex(r.To, -1) {
		if err := malformedRef(r.To[last:loc[0]]); err != nil {
			return err
		}
		last = loc[1]
		switch ref := decodeRef(r.To, loc); {
		case ref.capture > 0:
			if groups := r.re.NumSubexp(); ref.capture > groups {
				return fmt.Errorf("$%d but pattern has %d capture groups",
					ref.capture, groups)
			}
			if _, ok := escapes[ref.escape]; !ok {
				return fmt.Errorf("unknown escape ${%d:%s}; use query, path, or raw",
					ref.capture, ref.escape)
			}
		case ref.name == "q":
			return errors.New("{{q}} is only valid in fallback; use a capture group")
		default:
			if _, ok := c.Vars[ref.name]; !ok {
				return fmt.Errorf("undefined var {{%s}}", ref.name)
			}
		}
	}
	return malformedRef(r.To[last:])
}

// malformedRef catches an annotated capture that placeholder could not read —
// ${10:path}, ${1:path, ${1:} — in the text between the refs it did read.
// Such a ref matches nothing, so without this it would reach the browser as
// literal text in the URL rather than being reported on save.
func malformedRef(text string) error {
	i := strings.Index(text, "${")
	if i < 0 {
		return nil
	}
	ref := text[i:]
	if end := strings.IndexByte(ref, '}'); end >= 0 {
		ref = ref[:end+1]
	}
	return fmt.Errorf("malformed capture %s; write ${1:path}, or $1 to escape "+
		"for a query parameter", ref)
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

// Suggestion is one entry the address bar can offer while a query is being
// typed. Target is where Query lands if submitted as it stands, and is empty
// when that is not yet decided — "m (.+)" can offer "m " but cannot know where
// it goes until the rest is typed.
type Suggestion struct {
	Query  string
	Desc   string
	Target string
}

// Suggest returns up to limit rules whose typed form starts with q, in config
// order. What a rule can offer is its literal prefix: for a literal rule that
// is the whole pattern, and for a regex rule it is as much of it as a person
// could type blind. A rule that does not begin with literal text — "(a|b)" —
// has nothing to offer and is skipped.
func (c *Config) Suggest(q string, limit int) []Suggestion {
	q = strings.TrimSpace(q)
	if q == "" || limit <= 0 {
		return nil
	}

	found := make([]Suggestion, 0, limit)
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.prefix == "" || !strings.HasPrefix(r.prefix, q) {
			continue
		}
		// A regex prefix can repeat an earlier rule verbatim, and a shadowed
		// duplicate can never fire. Either way the second entry is noise.
		if slices.ContainsFunc(found, func(o Suggestion) bool {
			return o.Query == r.prefix
		}) {
			continue
		}
		s := Suggestion{Query: r.prefix, Desc: r.Desc, Target: c.settled(r.prefix)}
		if found = append(found, s); len(found) == limit {
			break
		}
	}
	return found
}

// settled reports where q lands if it is submitted exactly as it stands, and
// returns "" when that is not yet decided. It asks the resolver rather than
// reading the offering rule's own target, because an earlier rule may shadow
// it — a suggestion that names a destination has to name the real one.
func (c *Config) settled(q string) string {
	r, _ := c.find(q, false)
	if r == nil || len(r.parts) > 0 {
		return ""
	}
	return r.target
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
			// Captures come from the address bar, so they are escaped. Which
			// escape was decided when the rule loaded; see escapes.
			target.WriteString(part.escape(q[start:end]))
		}
	}
	return target.String()
}
