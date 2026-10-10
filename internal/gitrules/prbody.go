package gitrules

import (
	"fmt"
	"regexp"
	"strings"
)

// RequiredSections are the headings a pull request body must carry, in
// the order the template presents them (spec.md §20.2). The checker
// matches a heading case-insensitively and tolerates surrounding
// markdown, because GitHub renders the body and authors reflow text.
var RequiredSections = []string{
	"what changed",
	"why",
	"specification",
	"tests",
	"validation",
	"risk",
	"documentation and adrs",
	"operational impact",
	"breaking / migration impact",
	"known limitations",
}

// issueLinkRe matches a GitHub issue reference: "Closes #12",
// "Fixes owner/repo#12", "Resolves #12". Only a real keyword counts, so
// a bare "#12" in prose does not satisfy linkage.
var issueLinkRe = regexp.MustCompile(`(?im)^\s*(?:clos\w*|fix\w*|resolv\w*)\s*:?\s+(?:[\w.-]+/[\w.-]+)?#(\d+)\b`)

// IssueReference extracts the first closing issue number from a pull
// request body.
func IssueReference(body string) (int, bool) {
	m := issueLinkRe.FindStringSubmatch(body)
	if m == nil {
		return 0, false
	}
	n := 0
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// headingRe matches an ATX heading or a bold pseudo-heading, which is
// how the repository's pull request template declares sections.
var headingRe = regexp.MustCompile(`(?im)^\s*(?:#{1,6}\s*|\*\*)([^#*\n]{1,80}?)(?:\*\*)?\s*$`)

// CheckPRBody verifies the parts of the pull request contract that can
// be checked without the network (spec.md §20.2). It is pure: the same
// body always produces the same issues.
//
// The exceptions are narrow and named: a GitHub-generated dependency
// pull request and an urgent security fix. Both are identified by an
// explicit marker in the body, so an exception is visible and auditable
// rather than a self-applied label.
func CheckPRBody(body string) []Issue {
	var issues []Issue
	add := func(sev Severity, rule, format string, args ...any) {
		issues = append(issues, Issue{Rule: rule, Severity: sev, Message: fmt.Sprintf(format, args...)})
	}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return []Issue{{Rule: "pr/body", Severity: Error,
			Message: "the pull request body is empty; fill in every section of the pull request template"}}
	}

	if IsExemptPRBody(trimmed) {
		return nil
	}

	if _, ok := IssueReference(trimmed); !ok {
		add(Error, "pr/link",
			`no linked issue; add "Closes #N" (or Fixes/Resolves) to the Why section`)
	}

	sections := sectionsOf(trimmed)
	for _, want := range RequiredSections {
		content, ok := sections[want]
		if !ok {
			add(Error, "pr/section", "missing section %q; every section of the pull request template is required", titleCase(want))
			continue
		}
		if strings.TrimSpace(stripComments(content)) == "" {
			add(Error, "pr/section", "section %q is empty; state the answer or write \"none\" with a reason", titleCase(want))
		}
	}
	return issues
}

// exemptMarkers are the two documented exceptions. They are literal
// strings in the body, so an exception is reviewable in the diff rather
// than hidden in a label.
var exemptMarkers = []string{
	"dependency-pull-request",
	"urgent-security-fix",
}

// IsExemptPRBody reports whether the body declares one of the two
// documented exceptions (spec.md §20.3).
func IsExemptPRBody(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range exemptMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// sectionsOf maps a lower-cased heading to the text that follows it.
func sectionsOf(body string) map[string]string {
	out := map[string]string{}
	matches := headingRe.FindAllStringSubmatchIndex(body, -1)
	for i, m := range matches {
		name := strings.ToLower(strings.TrimSpace(strings.Trim(body[m[2]:m[3]], "*#")))
		end := len(body)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		out[name] = body[m[1]:end]
	}
	return out
}

// stripComments removes HTML comments, which the template uses for
// guidance and which do not count as an answer.
func stripComments(s string) string {
	var b strings.Builder
	for {
		open := strings.Index(s, "<!--")
		if open < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:open])
		close := strings.Index(s[open:], "-->")
		if close < 0 {
			return b.String()
		}
		s = s[open+close+3:]
	}
}

func titleCase(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
