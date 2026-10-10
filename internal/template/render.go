package template

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// placeholderRe matches one placeholder occurrence and captures the
// variable name. The grammar is deliberately narrow: "{{", optional
// whitespace, ".", a Go identifier, optional whitespace, "}}".
var placeholderRe = regexp.MustCompile(`\{\{\s*\.([A-Za-z][A-Za-z0-9]*)\s*\}\}`)

// Variable constraints. Every value a caller supplies is checked against
// these before it is rendered anywhere, so a value can never corrupt
// generated YAML, GitHub configuration, or Markdown structure by
// introducing a quote, a newline, or another placeholder.
var varConstraints = map[string]varConstraint{
	"ProjectName": {
		what:   "a project name (letters, digits, dot, underscore, hyphen; at most 100 characters)",
		maxLen: 100,
		allow:  func(r rune) bool { return isAlnum(r) || r == '.' || r == '_' || r == '-' },
		lead:   func(r rune) bool { return isAlnum(r) },
	},
	"Owner": {
		what:   "an owner login (letters, digits, underscore, hyphen; at most 100 characters)",
		maxLen: 100,
		allow:  func(r rune) bool { return isAlnum(r) || r == '_' || r == '-' },
		lead:   func(r rune) bool { return isAlnum(r) },
	},
	"RepoURL": {
		what:   "an http(s) URL",
		maxLen: 300,
		allow:  func(r rune) bool { return r > ' ' && r < 0x7f },
		lead:   func(r rune) bool { return r == 'h' },
		url:    true,
	},
	"Description": {
		what:   "a one-line description (printable, no newlines; 1-200 characters)",
		maxLen: 200,
		allow:  func(r rune) bool { return (r >= ' ' && r < 0x7f) || r >= 0xa0 },
		lead:   func(r rune) bool { return r >= ' ' },
	},
	"StandardVersion": {
		what: "a schema version vN",
		max: func(s string) bool {
			return versionRe.MatchString(s)
		},
		maxLen: 16,
		allow: func(r rune) bool {
			return (r >= '0' && r <= '9') || r == 'v'
		},
		lead: func(r rune) bool { return r == 'v' },
	},
}

var versionRe = regexp.MustCompile(`^v[0-9]+$`)

type varConstraint struct {
	what   string
	maxLen int
	allow  func(rune) bool
	lead   func(rune) bool
	// url additionally requires a parseable http(s) URL with a host.
	url bool
	// max is an optional whole-value predicate.
	max func(string) bool
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// ValidateVar checks one template variable name and value. It returns
// nil when the pair is safe to render. Names outside KnownVars and
// values that are empty, oversized, or contain control characters,
// whitespace, or placeholder syntax are rejected.
func ValidateVar(name, value string) error {
	c, ok := varConstraints[name]
	if !ok {
		return fmt.Errorf("template: unknown variable %q (known: %s)", name, strings.Join(KnownVars, ", "))
	}
	if value == "" {
		return fmt.Errorf("template: variable %q: value must not be empty", name)
	}
	if c.maxLen > 0 && len([]rune(value)) > c.maxLen {
		return fmt.Errorf("template: variable %q: value is longer than %d characters", name, c.maxLen)
	}
	if c.max != nil && !c.max(value) {
		return fmt.Errorf("template: variable %q: value must be %s (got %q)", name, c.what, value)
	}
	if c.lead != nil && !c.lead([]rune(value)[0]) {
		return fmt.Errorf("template: variable %q: value must be %s (got %q)", name, c.what, value)
	}
	for _, r := range value {
		if c.allow != nil && !c.allow(r) {
			return fmt.Errorf("template: variable %q: value must be %s (got %q)", name, c.what, value)
		}
	}
	// No value may reintroduce placeholder syntax: that would let one
	// value expand another, making output order-dependent.
	if strings.Contains(value, "{{") || strings.Contains(value, "}}") {
		return fmt.Errorf("template: variable %q: value must not contain placeholder syntax (got %q)", name, value)
	}
	if c.url {
		u := strings.TrimSuffix(value, "/")
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return fmt.Errorf("template: variable %q: value must be %s (got %q)", name, c.what, value)
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
		slash := strings.IndexByte(rest, '/')
		host := rest
		if slash >= 0 {
			host = rest[:slash]
		}
		if host == "" || strings.ContainsAny(host, " \t\"'") {
			return fmt.Errorf("template: variable %q: value must be %s (got %q)", name, c.what, value)
		}
	}
	return nil
}

// ValidateVars checks every supplied variable. Unknown keys and invalid
// values are reported together so the operator fixes one round trip,
// not five.
func ValidateVars(vars map[string]string) error {
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	var errs []string
	for _, k := range names {
		if err := ValidateVar(k, vars[k]); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("template: invalid variables: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Counts maps a variable name to the number of substitutions it
// performed in one document.
type Counts map[string]int

// Render substitutes every placeholder in text with the literal bytes
// of its value and reports how often each variable matched.
//
// Values are inserted literally: no regular-expression replacement
// semantics apply, so a value containing "$", a backslash, a digit
// after a dollar, or any other special character is copied verbatim.
//
// Rendering walks the document once, left to right, and never re-examines
// text it has already emitted. A value that happens to contain another
// variable's placeholder is therefore never expanded, and the result
// does not depend on map iteration order (spec.md §13.5).
func Render(text string, vars map[string]string) (string, Counts) {
	if len(vars) == 0 || text == "" {
		return text, nil
	}
	known := make(map[string]bool, len(KnownVars))
	for _, v := range KnownVars {
		known[v] = true
	}

	var b strings.Builder
	var counts Counts
	rest := text
	for {
		loc := placeholderRe.FindStringIndex(rest)
		if loc == nil {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:loc[0]])
		name := placeholderRe.FindStringSubmatch(rest[loc[0]:loc[1]])[1]
		if !known[name] {
			// Unknown placeholders are a validation error elsewhere;
			// never silently drop or replace them here.
			b.WriteString(rest[loc[0]:loc[1]])
			rest = rest[loc[1]:]
			continue
		}
		value, ok := vars[name]
		if !ok {
			// Unprovided variable: the placeholder stays, for
			// adoption-time resolution.
			b.WriteString(rest[loc[0]:loc[1]])
			rest = rest[loc[1]:]
			continue
		}
		b.WriteString(value)
		if counts == nil {
			counts = Counts{}
		}
		counts[name]++
		rest = rest[loc[1]:]
	}
	return b.String(), counts
}

// RenderFiles renders every non-binary file in files and returns the
// result. Input files are never modified: each rendered file is a fresh
// SourceFile with its own data slice.
func RenderFiles(files []SourceFile, vars map[string]string) []SourceFile {
	out := make([]SourceFile, len(files))
	for i, f := range files {
		out[i] = f
		if isBinary(f.Data) {
			continue
		}
		text, counts := Render(string(f.Data), vars)
		if len(counts) == 0 {
			continue
		}
		data := make([]byte, len(text))
		copy(data, text)
		out[i].Data = data
	}
	return out
}

// Describe renders counts in a stable order: files in the order given,
// variables in KnownVars order. It never depends on map iteration.
func Describe(counts Counts) string {
	names := make([]string, 0, len(counts))
	for _, v := range KnownVars {
		if n := counts[v]; n > 0 {
			names = append(names, fmt.Sprintf("%s×%d", v, n))
		}
	}
	return strings.Join(names, ", ")
}
