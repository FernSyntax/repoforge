// Package brand holds the product-identity gate shared by repository
// validation and template validation.
//
// RepoForge began a fresh product history at v0.1.0: the repository is
// not a continuation of an earlier product, and its tracked files and
// generated output must not carry the earlier product's name, module
// path, or repository URLs. Every place that could reintroduce one —
// source, docs, schemas, workflows, templates, generated output — goes
// through this single gate so the rule cannot drift between checkers.
//
// The patterns are assembled from fragments so this file does not
// contain the string it forbids: a gate that matched itself would
// either fail on every run or need an exemption that weakens it.
package brand

import (
	"regexp"
	"strings"
)

// Patterns are case-insensitive and tolerate the separator variants
// that show up in prose, identifiers, module paths, and URLs.
var patterns = []*regexp.Regexp{
	// The misspelling is included deliberately: it is the variant that
	// survived longest in shell scripts, because it reads as a word and
	// no human eye catches it.
	regexp.MustCompile(`(?i)` + "re" + `pro[-_ ]?` + "doctrine"),
	regexp.MustCompile(`(?i)` + "repo" + `[-_ ]?` + "doctrine"),
	regexp.MustCompile(`(?i)` + "doctrine" + `[-_ ]?` + "repo"),
}

// Names are the human-readable identities the patterns stand for, used
// in findings so an operator knows what to remove.
var names = []string{
	"the legacy product name (misspelled)",
	"the legacy product name",
	"the legacy product name in reverse order",
}

// Legacy is the legacy module path prefix. Callers that need to reason
// about import paths use it instead of hardcoding the string.
const LegacyModulePrefix = "github.com/" + "fernsyntax/repo" + "-doctrine"

// Find returns a message for every legacy-brand occurrence in text.
func Find(text string) []string {
	var out []string
	for i, re := range patterns {
		if m := re.FindString(text); m != "" {
			out = append(out, "contains "+m+" ("+names[i]+"); RepoForge v0.1.0 begins a fresh product history and must not reference the legacy product")
		}
	}
	return out
}

// CheckText is Find with the caller's subject already known; it is the
// convenience form for loops over files.
func CheckText(text string) []string { return Find(text) }

// Clean reports whether text carries no legacy branding.
func Clean(text string) bool { return len(Find(text)) == 0 }

// Normalize is a helper for tests and fixtures that need to build the
// forbidden strings without writing them literally.
func Normalize(parts ...string) string { return strings.Join(parts, "") }
