// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"errors"
	"os"
	"strings"
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

// TestLoadGovernanceProfileNames pins profile resolution: every
// profile the governance schema enumerates (baseline, solo, team,
// strict) must load through the strict decoder, and an unknown
// profile must be rejected by name — doctor resolves profiles by
// this exact set, so a typo here would silently bootstrap the wrong
// ruleset.
func TestLoadGovernanceProfileNames(t *testing.T) {
	doc := func(profile string) string {
		return "version: v1\nprofile: " + profile +
			"\nrepository:\n  features:\n    issues: true\n    wiki: true\n"
	}
	for _, profile := range []string{"baseline", "solo", "team", "strict"} {
		g, err := LoadGovernance(strings.NewReader(doc(profile)))
		if err != nil {
			t.Errorf("profile %q: %v", profile, err)
			continue
		}
		if g.Profile != profile {
			t.Errorf("profile = %q, want %q", g.Profile, profile)
		}
	}
	_, err := LoadGovernance(strings.NewReader(doc("wizard")))
	if err == nil {
		t.Fatal("an unknown profile must be rejected")
	}
	if !strings.Contains(err.Error(), `unknown profile "wizard" (want baseline, solo, team, strict)`) {
		t.Errorf("error = %q, want it to name the unknown profile and the valid set", err)
	}
}

// TestLoadGovernanceVersionCheck pins that only schema version v1 is
// accepted: a future document version must fail loudly rather than
// decode into a shape this tool misreads.
func TestLoadGovernanceVersionCheck(t *testing.T) {
	_, err := LoadGovernance(strings.NewReader(
		"version: v2\nprofile: solo\nrepository:\n  features:\n    issues: true\n    wiki: true\n"))
	if err == nil {
		t.Fatal("an unknown governance version must be rejected")
	}
	if !strings.Contains(err.Error(), `unknown version "v2"`) {
		t.Errorf("error = %q, want it to name the version", err)
	}
}

// errReader fails every read, standing in for an unreadable source
// (a closed file, a broken pipe) so the read-error path is covered
// deterministically without touching the filesystem.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

// TestLoadGovernanceReadError pins that a source that cannot be read
// at all surfaces its error instead of being mistaken for an empty
// (and therefore invalid) document.
func TestLoadGovernanceReadError(t *testing.T) {
	if _, err := LoadGovernance(errReader{}); err == nil {
		t.Fatal("an unreadable source must fail")
	}
}

// TestLoadGovernanceParseError pins the parse-error wrap: a document
// that is not decodable YAML fails with the "governance document:"
// prefix so callers can tell a broken file from a rejected
// invariant.
func TestLoadGovernanceParseError(t *testing.T) {
	_, err := LoadGovernance(strings.NewReader("version: [v1\n"))
	if err == nil {
		t.Fatal("malformed YAML must be rejected")
	}
	if !strings.Contains(err.Error(), "governance document:") {
		t.Errorf("error = %q, want the governance document prefix", err)
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
