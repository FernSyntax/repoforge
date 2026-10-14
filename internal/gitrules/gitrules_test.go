// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package gitrules

import "testing"

func TestCheckCommitMessage(t *testing.T) {
	cases := []struct {
		name        string
		message     string
		wantErrors  int
		wantWarning bool
		rule        string // expected rule of first error, when wantErrors > 0
	}{
		{name: "plain feat", message: "feat: add user login", wantErrors: 0},
		{name: "with scope", message: "fix(cli): handle empty config", wantErrors: 0},
		{name: "breaking without scope", message: "feat!: drop legacy mode", wantErrors: 0},
		{name: "breaking with scope", message: "refactor(api)!: rename endpoints", wantErrors: 0},
		{name: "security type", message: "security: patch token handling", wantErrors: 0},
		{name: "revert type", message: "revert: feat: add user login", wantErrors: 0},
		{name: "multiline uses subject", message: "docs: update readme\n\nLong body here.", wantErrors: 0},
		{name: "unknown type", message: "feature: add login", wantErrors: 1, rule: "type"},
		{name: "capitalized type", message: "Feat: add login", wantErrors: 1, rule: "type"},
		{name: "no colon", message: "add login", wantErrors: 1, rule: "format"},
		{name: "empty subject", message: "fix: ", wantErrors: 1, rule: "subject"},
		{name: "uppercase scope", message: "fix(API): handle error", wantErrors: 1, rule: "scope"},
		{name: "trailing period", message: "fix: handle error.", wantErrors: 1, rule: "period"},
		{name: "too long", message: "feat: add this very long subject that just keeps going and going past the limit", wantErrors: 0, wantWarning: true},
		{name: "generic fix stuff", message: "fix: fix stuff", wantErrors: 1, rule: "generic"},
		{name: "generic updates", message: "chore: updates", wantErrors: 1, rule: "generic"},
		{name: "generic changes", message: "chore: changes", wantErrors: 1, rule: "generic"},
		{name: "generic wip", message: "feat: wip", wantErrors: 1, rule: "generic"},
		{name: "generic misc", message: "chore: misc", wantErrors: 1, rule: "generic"},
		{name: "generic temp", message: "test: temp", wantErrors: 1, rule: "generic"},
		{name: "generic case-insensitive", message: "chore: WIP", wantErrors: 1, rule: "generic"},
		{name: "non-imperative past tense", message: "feat: added login", wantErrors: 0, wantWarning: true},
		{name: "non-imperative gerund", message: "fix: fixing the parser", wantErrors: 0, wantWarning: true},
		{name: "non-imperative third person", message: "docs: updates the guide", wantErrors: 0, wantWarning: true},
		{name: "warning does not fail", message: "feat: improved caching", wantErrors: 0, wantWarning: true},
		{name: "github test-merge exempt", message: "Merge bd46a23c5cd8a363f3ddc4f0bc6ce5218823b111 into 3b7a4cd9d1916083eae3d821615122a7a7e8017c", wantErrors: 0},
		{name: "human merge still fails", message: "Merge branch 'main' into feat/1-thing", wantErrors: 1, rule: "format"},
		{name: "short sha merge still fails", message: "Merge bd46a23 into main", wantErrors: 1, rule: "format"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := CheckCommitMessage(c.message)
			var errors, warnings int
			var firstRule string
			for _, i := range issues {
				switch i.Severity {
				case Error:
					if errors == 0 {
						firstRule = i.Rule
					}
					errors++
				case Warning:
					warnings++
				}
			}
			if c.wantErrors > 0 {
				if errors == 0 {
					t.Fatalf("expected error(s), got none (issues: %+v)", issues)
				}
				if firstRule != c.rule {
					t.Errorf("first error rule = %q, want %q (issues: %+v)", firstRule, c.rule, issues)
				}
			} else if errors > 0 {
				t.Fatalf("expected no errors, got: %+v", issues)
			}
			if c.wantWarning && warnings == 0 {
				t.Errorf("expected a warning, got none (issues: %+v)", issues)
			}
		})
	}
}

func TestCheckCommitMessageLengthBoundary(t *testing.T) {
	// Exactly 72 chars after "feat: " must pass cleanly; 73 draws a
	// length warning but never fails (C2 is Recommended).
	base := "feat: "
	subject72 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if len(subject72) != 72 {
		t.Fatalf("test setup: %d", len(subject72))
	}
	if Failed(CheckCommitMessage(base + subject72)) {
		t.Error("72-char subject should pass")
	}
	long := CheckCommitMessage(base + subject72 + "a")
	if Failed(long) {
		t.Errorf("73-char subject should warn, not fail: %+v", long)
	}
	var warned bool
	for _, i := range long {
		if i.Severity == Warning && i.Rule == "length" {
			warned = true
		}
	}
	if !warned {
		t.Errorf("73-char subject should carry a length warning: %+v", long)
	}
}

func TestCheckBranchName(t *testing.T) {
	cases := []struct {
		name   string
		branch string
		wantOK bool
	}{
		{name: "main", branch: "main", wantOK: true},
		{name: "feat", branch: "feat/12-user-login", wantOK: true},
		{name: "docs", branch: "docs/3-guide", wantOK: true},
		{name: "security", branch: "security/77-token-rotation", wantOK: true},
		{name: "multi word", branch: "fix/101-fix-the-parser-bug", wantOK: true},
		{name: "revert is not a branch type", branch: "revert/12-thing", wantOK: false},
		{name: "no type prefix", branch: "feature", wantOK: false},
		{name: "missing issue", branch: "feat/user-login", wantOK: false},
		{name: "issue zero", branch: "feat/0-x", wantOK: false},
		{name: "missing description", branch: "feat/12", wantOK: false},
		{name: "uppercase", branch: "Feat/12-x", wantOK: false},
		{name: "uppercase description", branch: "feat/12-Thing", wantOK: false},
		{name: "underscore", branch: "feat/12-bad_name", wantOK: false},
		{name: "missing dash", branch: "feat/12thing", wantOK: false},
		{name: "dependabot go module", branch: "dependabot/go_modules/gopkg.in/yaml.v3-3.0.1", wantOK: true},
		{name: "dependabot actions", branch: "dependabot/github_actions/actions/checkout-7", wantOK: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := CheckBranchName(c.branch, "main")
			if c.wantOK && Failed(issues) {
				t.Errorf("expected valid, got: %+v", issues)
			}
			if !c.wantOK && !Failed(issues) {
				t.Errorf("expected invalid, got no errors")
			}
		})
	}
}

func TestCheckBranchNameCustomDefault(t *testing.T) {
	if Failed(CheckBranchName("trunk", "trunk")) {
		t.Error("custom default branch should be valid")
	}
	if !Failed(CheckBranchName("main", "trunk")) {
		t.Error("main should be invalid when default is trunk")
	}
}
