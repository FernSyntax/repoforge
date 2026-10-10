package gitrules

import (
	"strings"
	"testing"
)

// fullBody is a body that satisfies every rule, used as the baseline
// for the negative cases so a failure is unambiguous.
func fullBody() string {
	return strings.Join([]string{
		"## What changed",
		"Added a thing.",
		"",
		"## Why",
		"Closes #7",
		"",
		"## Specification",
		"FR-011, NFR-001",
		"",
		"## Tests",
		"manifest_test.go",
		"",
		"## Validation",
		"gofmt -l . (clean)",
		"",
		"## Risk",
		"none",
		"",
		"## Documentation and ADRs",
		"spec.md §13",
		"",
		"## Operational impact",
		"none",
		"",
		"## Breaking / migration impact",
		"None",
		"",
		"## Known limitations",
		"none",
	}, "\n")
}

// TestCheckPRBodyAcceptsACompleteBody proves the happy path, including
// a body whose guidance comments are stripped before emptiness is
// judged.
func TestCheckPRBodyAcceptsACompleteBody(t *testing.T) {
	if issues := CheckPRBody(fullBody()); len(issues) != 0 {
		t.Fatalf("a complete body must pass, got: %s", joinMessages(issues))
	}
	withGuidance := "<!-- fill this in -->\n" + fullBody()
	if issues := CheckPRBody(withGuidance); len(issues) != 0 {
		t.Fatalf("guidance comments must not count as content: %s", joinMessages(issues))
	}
}

// TestCheckPRBodyRejectsMissingLink proves a body without a closing
// keyword is not linked, and a bare "#12" is not enough.
func TestCheckPRBodyRejectsMissingLink(t *testing.T) {
	body := strings.NewReplacer("Closes #7", "see #7").Replace(fullBody())
	if issues := CheckPRBody(body); !hasRuleN(issues, "pr/link") {
		t.Errorf("a body without a closing keyword must be reported: %s", joinMessages(issues))
	}
	if _, ok := IssueReference("see #7 for context"); ok {
		t.Error("a bare issue reference must not count as linkage")
	}
	for _, body := range []string{"Closes #12", "fixes #3", "Resolves: #9", "Closes owner/repo#4"} {
		if n, ok := IssueReference(body); !ok || n <= 0 {
			t.Errorf("IssueReference(%q) = %d, %v", body, n, ok)
		}
	}
}

// TestCheckPRBodyRejectsMissingAndEmptySections proves both failure
// modes: a heading that is absent, and a heading with nothing under it.
func TestCheckPRBodyRejectsMissingAndEmptySections(t *testing.T) {
	missing := strings.NewReplacer("## Known limitations\nnone", "").Replace(fullBody())
	if issues := CheckPRBody(missing); !hasRuleN(issues, "pr/section") ||
		!strings.Contains(joinMessages(issues), "Known Limitations") {
		t.Errorf("a missing section must be named: %s", joinMessages(issues))
	}

	empty := strings.NewReplacer("## Risk\nnone", "## Risk\n").Replace(fullBody())
	issues := CheckPRBody(empty)
	if !hasRuleN(issues, "pr/section") || !strings.Contains(joinMessages(issues), "empty") {
		t.Errorf("an empty section must be reported: %s", joinMessages(issues))
	}
}

// TestCheckPRBodyRejectsEmptyBody proves the degenerate case.
func TestCheckPRBodyRejectsEmptyBody(t *testing.T) {
	for _, body := range []string{"", "   \n\n  ", "<!-- only comments -->"} {
		if issues := CheckPRBody(body); len(issues) == 0 {
			t.Errorf("body %q must be reported", body)
		}
	}
}

// TestPRBodyExceptionsAreNarrow proves exactly the two documented
// exceptions are recognised, and that they are the only ones.
func TestPRBodyExceptionsAreNarrow(t *testing.T) {
	for _, marker := range []string{"dependency-pull-request", "urgent-security-fix"} {
		body := "## Whatever\n\nChangelog: dependencies\n" + marker + "\n"
		if !IsExemptPRBody(body) {
			t.Errorf("%q must be recognised", marker)
		}
		if issues := CheckPRBody(body); len(issues) != 0 {
			t.Errorf("a declared exception must pass, got: %s", joinMessages(issues))
		}
	}
	for _, body := range []string{"", "skip process", "WIP", "trivial", "docs only", "I said so"} {
		if IsExemptPRBody(body) {
			t.Errorf("%q must not be an exception", body)
		}
	}
}

func hasRuleN(issues []Issue, rule string) bool {
	for _, i := range issues {
		if i.Rule == rule {
			return true
		}
	}
	return false
}

func joinMessages(issues []Issue) string {
	var parts []string
	for _, i := range issues {
		parts = append(parts, i.Rule+": "+i.Message)
	}
	return strings.Join(parts, " | ")
}
