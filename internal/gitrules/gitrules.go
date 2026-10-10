// Package gitrules implements RepoForge's commit message and branch
// naming rules (docs/development/commit-conventions.md,
// docs/development/branching.md). It is the single source of truth for
// these rules: the validate command, the git commit-check/branch-check
// commands, and pr check all consume it.
package gitrules

import (
	"fmt"
	"regexp"
	"strings"
)

// Severity of a rule finding. Warnings are guidance and never fail a
// check; Errors do.
type Severity string

const (
	Error   Severity = "ERROR"
	Warning Severity = "WARN"
)

// Issue is one rule finding.
type Issue struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

func (i Issue) String() string {
	return fmt.Sprintf("%s %s: %s", i.Severity, i.Rule, i.Message)
}

// Types are the allowed commit types (commit-conventions.md C1).
var Types = []string{
	"feat", "fix", "docs", "refactor", "perf", "test",
	"build", "ci", "chore", "revert", "security",
}

// BranchTypes are the allowed branch name prefixes (branching.md B3).
// Note: revert is a commit type, never a branch type.
var BranchTypes = []string{
	"feat", "fix", "refactor", "perf", "test", "docs",
	"build", "ci", "security", "chore",
}

const MaxSubjectLength = 72

var (
	commitRe = regexp.MustCompile(`^(?:` + strings.Join(Types, "|") + `)(\([a-z0-9][a-z0-9-]*\))?(!)?:[ ]?(.*)$`)
	looseRe  = regexp.MustCompile(`^([A-Za-z]+)(\(([^)]*)\))?(!)?:(.*)$`)
	scopeRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

	branchRe     = regexp.MustCompile(`^(?:` + strings.Join(BranchTypes, "|") + `)/[1-9][0-9]*-[a-z0-9]+(-[a-z0-9]+)*$`)
	automationRe = regexp.MustCompile(`^dependabot/[A-Za-z0-9._/-]+$`)

	// githubMergeRe matches GitHub's synthetic test-merge commits
	// ("Merge <40-hex> into <40-hex>"), synthesized for
	// pull_request CI runs when checkout tests the merge result.
	// Machine-generated, never human: human merges name branches
	// ("Merge branch 'x' …"), never two full SHAs. Exempt so
	// required CI validates the merged result instead of failing on
	// the harness itself.
	githubMergeRe = regexp.MustCompile(`^Merge [0-9a-f]{40} into [0-9a-f]{40}$`)
)

// genericSubjects are banned low-information subjects (normalized
// lowercase, trailing punctuation stripped).
var genericSubjects = map[string]bool{
	"fix stuff": true, "updates": true, "update": true,
	"changes": true, "change": true, "wip": true,
	"work in progress": true, "misc": true, "temp": true, "tmp": true,
}

// nonImperativeStarts are common non-imperative first words flagged as
// style guidance (Warning, never Error).
var nonImperativeStarts = map[string]string{
	"added": "add", "adds": "add", "adding": "add",
	"fixed": "fix", "fixes": "fix", "fixing": "fix",
	"updated": "update", "updating": "update",
	"changed": "change", "changes": "change", "changing": "change",
	"removed": "remove", "removes": "remove", "removing": "remove",
	"updates": "update", "improved": "improve", "improves": "improve",
	"improving": "improve",
	"deleted":   "delete", "deletes": "delete", "deleting": "delete",
	"created": "create", "creates": "create", "creating": "create",
	"implemented": "implement", "implements": "implement",
	"renamed": "rename", "renaming": "rename",
	"moved": "move", "moving": "move",
}

// CheckCommitMessage validates a commit message subject line against
// the restricted Conventional Commits subset. Only the first line is
// inspected. Breaking changes via the `!` marker are fully supported.
func CheckCommitMessage(message string) []Issue {
	var issues []Issue
	add := func(sev Severity, rule, format string, args ...any) {
		issues = append(issues, Issue{Rule: rule, Severity: sev, Message: fmt.Sprintf(format, args...)})
	}

	subject := strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])

	if githubMergeRe.MatchString(subject) {
		return nil
	}

	m := commitRe.FindStringSubmatch(subject)
	if m == nil {
		// Diagnose why it failed to match.
		lm := looseRe.FindStringSubmatch(subject)
		switch {
		case lm == nil:
			add(Error, "format", "must match <type>(<optional scope>)!: <subject>; got %q", subject)
		case !contains(Types, lm[1]):
			add(Error, "type", "unknown type %q (allowed: %s)", lm[1], strings.Join(Types, ", "))
		case lm[3] != "" && !scopeRe.MatchString(lm[3]):
			add(Error, "scope", "scope %q must be lowercase alphanumerics and hyphens", lm[3])
		case strings.TrimSpace(lm[5]) == "":
			add(Error, "subject", "subject is required")
		default:
			add(Error, "format", "must match <type>(<optional scope>)!: <subject>; got %q", subject)
		}
		return issues
	}

	subject = m[3]
	if strings.TrimSpace(subject) == "" {
		add(Error, "subject", "subject is required")
		return issues
	}
	if len(subject) > MaxSubjectLength {
		// Guidance, not failure (commit-conventions.md C2 is
		// Recommended): a hard length gate once held a merged main
		// red over a single character, which costs more than the
		// style breach. Same level as imperative mood.
		add(Warning, "length", "subject is %d characters; maximum is %d", len(subject), MaxSubjectLength)
	}
	if strings.HasSuffix(subject, ".") {
		add(Error, "period", "subject must not end with a period")
	}
	normalized := strings.TrimRight(strings.ToLower(strings.TrimSpace(subject)), ".!")
	if genericSubjects[normalized] {
		add(Error, "generic", "subject %q is too generic to be useful; say what changed and why", subject)
	}
	first := strings.ToLower(strings.Fields(subject)[0])
	if imperative, ok := nonImperativeStarts[first]; ok {
		add(Warning, "imperative", "prefer imperative mood: %q → %q (e.g. \"%s …\")", first, imperative, imperative)
	}
	return issues
}

// CheckBranchName validates a branch name: the default branch, the
// <type>/<issue>-<description> pattern, or a recognized automation
// branch (Dependabot).
func CheckBranchName(branch, defaultBranch string) []Issue {
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	if branch == defaultBranch {
		return nil
	}
	if automationRe.MatchString(branch) {
		return nil
	}
	if branchRe.MatchString(branch) {
		return nil
	}
	return []Issue{{
		Rule:     "format",
		Severity: Error,
		Message:  fmt.Sprintf("branch %q must be %q, a Dependabot branch, or match <type>/<issue>-<description> (type: %s)", branch, defaultBranch, strings.Join(BranchTypes, ", ")),
	}}
}

// Failed reports whether any issue is an Error.
func Failed(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == Error {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
