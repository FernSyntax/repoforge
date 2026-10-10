package config

import (
	"bytes"
	"os"
	"testing"
)

// TestEmbeddedGovernanceProfilesParse loads every embedded governance
// document with the strict (KnownFields) decoder. This guards the
// struct ↔ schema correspondence: a governance document that parses
// must not be rejected for a field the schema defines (e.g. the
// strict profile's metadata_restrictions).
func TestEmbeddedGovernanceProfilesParse(t *testing.T) {
	wantProfile := map[string]string{
		"repository": "baseline",
		"solo":       "solo",
		"team":       "team",
		"strict":     "strict",
	}
	for name, profile := range wantProfile {
		data, err := os.ReadFile("../../configs/github/" + name + ".yaml")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		g, err := LoadGovernance(bytes.NewReader(data))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if g.Profile != profile {
			t.Errorf("%s: profile = %q, want %q", name, g.Profile, profile)
		}
	}
}

// TestStrictProfilePatternRules pins the Enterprise-only pattern
// rules: they must round-trip through the strict document.
func TestStrictProfilePatternRules(t *testing.T) {
	data, err := os.ReadFile("../../configs/github/strict.yaml")
	if err != nil {
		t.Fatal(err)
	}
	g, err := LoadGovernance(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	p := g.Rulesets.Main.Rules.MetadataRestrictions.CommitMessagePattern
	if p == nil {
		t.Fatal("strict: rulesets.main.rules.metadata_restrictions.commit_message_pattern missing")
	}
	if p.Operator != "regex" || p.Pattern == "" {
		t.Fatalf("commit message pattern = %+v", p)
	}
	tag := g.Rulesets.ReleaseTags.Rules.TagNamePattern
	if tag == nil {
		t.Fatal("strict: rulesets.release_tags.rules.tag_name_pattern missing")
	}
	if tag.Operator != "regex" || tag.Pattern == "" {
		t.Fatalf("tag name pattern = %+v", tag)
	}
}

// TestGovernanceProfilesRequireIssuesAndWiki pins the governance
// invariant the RepoForge standard depends on: a profile that disables
// Issues or the wiki is not a RepoForge profile, because the
// issue-driven workflow and the seeded wiki both need them.
func TestGovernanceProfilesRequireIssuesAndWiki(t *testing.T) {
	for _, body := range []string{
		"version: v1\nprofile: solo\nrepository:\n  features:\n    issues: true\n    wiki: false\n",
		"version: v1\nprofile: solo\nrepository:\n  features:\n    issues: false\n    wiki: true\n",
	} {
		if _, err := LoadGovernance(bytes.NewReader([]byte(body))); err == nil {
			t.Errorf("a profile that disables Issues or the wiki must be rejected:\n%s", body)
		}
	}
	if _, err := LoadGovernance(bytes.NewReader([]byte(
		"version: v1\nprofile: solo\nrepository:\n  features:\n    issues: true\n    wiki: true\n"))); err != nil {
		t.Errorf("a conforming profile must load: %v", err)
	}
	if _, err := LoadGovernance(bytes.NewReader([]byte(
		"version: v1\nprofile: nope\nrepository:\n  features:\n    issues: true\n    wiki: true\n"))); err == nil {
		t.Error("an unknown profile must be rejected")
	}
}

// TestGovernanceOptInPolicyRules pins the policy-level enforcement: a
// repository that declares a governance profile must keep Issues and
// the wiki enabled, while a repository with no profile is free to set
// either.
func TestGovernanceOptInPolicyRules(t *testing.T) {
	withProfile := func(github string) *Policy {
		p := &Policy{}
		p.Standard.Version = SchemaVersion
		p.Repository.GovernanceProfile = "solo"
		p.GitHub.Wiki = true
		if github == "issues-off" {
			off := false
			p.GitHub.Issues = &off
		}
		if github == "wiki-off" {
			p.GitHub.Wiki = false
		}
		return p
	}
	if v := ValidatePolicy(withProfile("issues-off")); len(v) == 0 {
		t.Error("declaring a profile with issues disabled must be a violation")
	}
	if v := ValidatePolicy(withProfile("wiki-off")); len(v) == 0 {
		t.Error("declaring a profile with the wiki disabled must be a violation")
	}
	if v := ValidatePolicy(withProfile("")); len(v) != 0 {
		t.Errorf("a conforming opt-in policy must be valid: %v", v)
	}

	// No governance profile: the standard does not apply.
	bare := &Policy{}
	bare.Standard.Version = SchemaVersion
	bare.GitHub.Wiki = false
	off := false
	bare.GitHub.Issues = &off
	if v := ValidatePolicy(bare); len(v) != 0 {
		t.Errorf("a policy without a governance profile must not be forced onto the standard: %v", v)
	}
}
