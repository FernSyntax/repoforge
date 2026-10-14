// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// TestPolicyTriStateAccessors pins the schema-default contract
// (spec.md §9.4): every field that defaults to true is a pointer so
// an omitted field (nil) stays distinguishable from an explicit
// false, and the accessor is the single place that resolves nil to
// true. Bootstrap and doctor derive desired repository state from
// these accessors, so a regression here silently flips live
// repository settings instead of failing a check.
func TestPolicyTriStateAccessors(t *testing.T) {
	cases := []struct {
		name string
		get  func(*Policy) bool
		set  func(*Policy, *bool)
	}{
		{"MergeSquash", (*Policy).MergeSquash, func(p *Policy, b *bool) { p.Merge.Squash = b }},
		{"MergeDeleteBranch", (*Policy).MergeDeleteBranch, func(p *Policy, b *bool) { p.Merge.DeleteBranch = b }},
		{"GitHubIssues", (*Policy).GitHubIssues, func(p *Policy, b *bool) { p.GitHub.Issues = b }},
		{"SecurityDependabot", (*Policy).SecurityDependabot, func(p *Policy, b *bool) { p.Security.Dependabot = b }},
		{"SecuritySecretScanning", (*Policy).SecuritySecretScanning, func(p *Policy, b *bool) { p.Security.SecretScanning = b }},
		{"SecuritySecretScanningPushProtection", (*Policy).SecuritySecretScanningPushProtection, func(p *Policy, b *bool) {
			p.Security.SecretScanningPushProtection = b
		}},
		{"SecurityDependencyReview", (*Policy).SecurityDependencyReview, func(p *Policy, b *bool) {
			p.Security.DependencyReview = b
		}},
		{"SecurityPrivateVulnerabilityReporting", (*Policy).SecurityPrivateVulnerabilityReporting, func(p *Policy, b *bool) {
			p.Security.PrivateVulnerabilityReporting = b
		}},
		{"SecurityScorecard", (*Policy).SecurityScorecard, func(p *Policy, b *bool) { p.Security.Scorecard = b }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.get(&Policy{}); !got {
				t.Errorf("unset %s() = false, want true (schema default)", c.name)
			}
			for _, explicit := range []bool{true, false} {
				p := &Policy{}
				c.set(p, boolPtr(explicit))
				if got := c.get(p); got != explicit {
					t.Errorf("%s() = %v for explicit %v, want %v", c.name, got, explicit, explicit)
				}
			}
		})
	}
}

// TestMergeMethods pins the merge-method derivation that the GitHub
// repository settings and the ruleset pull_request rule must both
// agree on (docs/architecture/github-governance.md): the list
// follows the merge flags in the documented order, and the squash
// entry follows the tri-state default, so an unset squash still
// allows squash merges.
func TestMergeMethods(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Policy)
		want   string
	}{
		{"schema default allows squash merges", nil, "squash"},
		{"merge commit only", func(p *Policy) {
			p.Merge.MergeCommit = true
			p.Merge.Squash = boolPtr(false)
		}, "merge"},
		{"rebase only", func(p *Policy) {
			p.Merge.Rebase = true
			p.Merge.Squash = boolPtr(false)
		}, "rebase"},
		{"every method keeps documented order", func(p *Policy) {
			p.Merge.MergeCommit = true
			p.Merge.Rebase = true
		}, "merge,squash,rebase"},
		{"no method enabled", func(p *Policy) { p.Merge.Squash = boolPtr(false) }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &Policy{}
			if c.mutate != nil {
				c.mutate(p)
			}
			if got := strings.Join(p.MergeMethods(), ","); got != c.want {
				t.Errorf("MergeMethods() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestActionPinningMode pins the action-pinning default: an unset
// security.action_pinning means full SHA pinning, the strictest
// mode, so workflows scaffolded without an explicit value must not
// fall back to tag pinning.
func TestActionPinningMode(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"", "sha"},
		{"sha", "sha"},
		{"tag", "tag"},
		{"none", "none"},
	}
	for _, c := range cases {
		p := &Policy{}
		p.Security.ActionPinning = c.mode
		if got := p.ActionPinningMode(); got != c.want {
			t.Errorf("ActionPinningMode() with %q = %q, want %q", c.mode, got, c.want)
		}
	}
}

// TestDefaultBranch pins the default-branch fallback: the contract
// omits repository.default_branch on most projects, and every
// ruleset and check context must then target main.
func TestDefaultBranch(t *testing.T) {
	cases := []struct {
		branch string
		want   string
	}{
		{"", "main"},
		{"main", "main"},
		{"develop", "develop"},
		{"release/1.x", "release/1.x"},
	}
	for _, c := range cases {
		p := &Policy{}
		p.Repository.DefaultBranch = c.branch
		if got := p.DefaultBranch(); got != c.want {
			t.Errorf("DefaultBranch() with %q = %q, want %q", c.branch, got, c.want)
		}
	}
}

// TestGovernanceProfileName pins the single place the governance
// profile default lives, including the nil-receiver path callers hit
// when a repository carries no policy at all: bootstrap and doctor
// must derive the same desired rulesets whether the profile is
// unset, defaulted, or explicit.
func TestGovernanceProfileName(t *testing.T) {
	var nilPolicy *Policy
	if got := nilPolicy.GovernanceProfileName(); got != "solo" {
		t.Errorf("nil policy GovernanceProfileName() = %q, want solo", got)
	}
	cases := []struct {
		profile string
		want    string
	}{
		{"", "solo"},
		{"solo", "solo"},
		{"team", "team"},
		{"strict", "strict"},
	}
	for _, c := range cases {
		p := &Policy{}
		p.Repository.GovernanceProfile = c.profile
		if got := p.GovernanceProfileName(); got != c.want {
			t.Errorf("GovernanceProfileName() with %q = %q, want %q", c.profile, got, c.want)
		}
	}
}

// TestViolationString pins the rendered violation form operators
// see: field-scoped problems are prefixed with their field path,
// while document-wide problems (loader messages) render bare.
func TestViolationString(t *testing.T) {
	cases := []struct {
		name string
		v    Violation
		want string
	}{
		{
			name: "field-scoped",
			v:    Violation{Field: "standard.version", Message: "required field missing"},
			want: "standard.version: required field missing",
		},
		{
			name: "document-wide",
			v:    Violation{Message: "yaml: line 2: did not find expected node content"},
			want: "yaml: line 2: did not find expected node content",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.v.String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
		})
	}
}
