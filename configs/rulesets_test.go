// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package configs

import (
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/config"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// TestCanonicalPayloads pins the ownership and shape invariants every
// canonical payload must hold: deterministic names, active
// enforcement, an explicit (empty) bypass list, and ref conditions.
func TestCanonicalPayloads(t *testing.T) {
	cases := []struct {
		file    string
		name    string
		target  string
		include []string
	}{
		{"main-solo", MainRulesetName, "branch", []string{"~DEFAULT_BRANCH"}},
		{"main-team", MainRulesetName, "branch", []string{"~DEFAULT_BRANCH"}},
		{"main-strict", MainRulesetName, "branch", []string{"~DEFAULT_BRANCH"}},
		{"tags", ReleaseTagsRulesetName, "tag", []string{"refs/tags/v*"}},
	}
	for _, c := range cases {
		p, err := RulesetPayload(c.file)
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		if p.Name != c.name {
			t.Errorf("%s: name = %q, want deterministic %q", c.file, p.Name, c.name)
		}
		if p.Target != c.target {
			t.Errorf("%s: target = %q, want %q", c.file, p.Target, c.target)
		}
		if p.Enforcement != "active" {
			t.Errorf("%s: enforcement = %q, want active", c.file, p.Enforcement)
		}
		if p.BypassActors == nil || len(p.BypassActors) != 0 {
			t.Errorf("%s: bypass_actors must be an explicit empty list, got %#v", c.file, p.BypassActors)
		}
		if p.Conditions == nil {
			t.Errorf("%s: conditions missing", c.file)
		} else {
			if got := strings.Join(p.Conditions.RefName.Include, ","); got != strings.Join(c.include, ",") {
				t.Errorf("%s: conditions include = %q, want %q", c.file, got, c.include)
			}
			if len(p.Conditions.RefName.Exclude) != 0 {
				t.Errorf("%s: conditions exclude = %v, want empty", c.file, p.Conditions.RefName.Exclude)
			}
		}
		if len(p.Rules) == 0 {
			t.Errorf("%s: no rules", c.file)
		}
	}
}

// TestTagsPayloadImmutable asserts the release tag ruleset enforces
// the documented immutability rules.
func TestTagsPayloadImmutable(t *testing.T) {
	p, err := RulesetPayload("tags")
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"deletion", "update"} {
		if ruleByType(p, typ) == nil {
			t.Errorf("tags payload missing %q rule", typ)
		}
	}
	// tag_name_pattern is Enterprise-only; the canonical payload
	// deliberately omits it (see configs/README.md).
	if ruleByType(p, "tag_name_pattern") != nil {
		t.Error("tags payload must not carry the Enterprise-only tag_name_pattern rule")
	}
}

func TestDesiredRulesetsOverlays(t *testing.T) {
	main, tags, err := DesiredRulesets("team", []string{"lint", "test"}, []string{"squash", "rebase"})
	if err != nil {
		t.Fatal(err)
	}
	sc := ruleByType(main, "required_status_checks")
	if sc == nil {
		t.Fatal("required_status_checks rule missing")
	}
	contexts := statusCheckContexts(sc)
	if strings.Join(contexts, ",") != "lint,test" {
		t.Errorf("contexts = %v, want [lint test]", contexts)
	}
	pr := ruleByType(main, "pull_request")
	if pr == nil {
		t.Fatal("pull_request rule missing")
	}
	if got := anyToStrings(pr.Parameters["allowed_merge_methods"]); strings.Join(got, ",") != "squash,rebase" {
		t.Errorf("allowed_merge_methods = %v, want [squash rebase]", got)
	}
	if tags == nil || tags.Name != ReleaseTagsRulesetName {
		t.Errorf("tags payload = %+v", tags)
	}

	// No CI contexts: the required_status_checks rule is dropped.
	main2, _, err := DesiredRulesets("team", nil, []string{"squash"})
	if err != nil {
		t.Fatal(err)
	}
	if ruleByType(main2, "required_status_checks") != nil {
		t.Error("required_status_checks must be dropped when no contexts are discovered")
	}

	if _, _, err := DesiredRulesets("baseline", nil, nil); err == nil {
		t.Error("unknown profile must be rejected")
	}
}

// TestGovernanceProfilesMatchCanonicalPayloads is the cross-check
// between the governance documents (the human-edited desired state,
// schemas/github-governance.schema.yaml) and the canonical ruleset
// payloads (the machine-applied desired state). Every field the
// schema defines and the payload realizes must agree — a divergence
// would make doctor and bootstrap answer differently for the same
// repository.
func TestGovernanceProfilesMatchCanonicalPayloads(t *testing.T) {
	for _, profile := range []string{"solo", "team", "strict"} {
		profile := profile
		t.Run(profile, func(t *testing.T) {
			data, err := GitHub.ReadFile("github/" + profile + ".yaml")
			if err != nil {
				t.Fatal(err)
			}
			gov, err := config.LoadGovernance(strings.NewReader(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			main, err := RulesetPayload("main-" + profile)
			if err != nil {
				t.Fatal(err)
			}
			tags, err := RulesetPayload("tags")
			if err != nil {
				t.Fatal(err)
			}
			checkMainAgainstGovernance(t, gov, main)
			checkTagsAgainstGovernance(t, gov, tags)
		})
	}
}

func checkMainAgainstGovernance(t *testing.T, gov *config.Governance, p *ghub.RulesetPayload) {
	t.Helper()
	m := gov.Rulesets.Main
	if m.Enforcement != p.Enforcement {
		t.Errorf("enforcement: governance %q != payload %q", m.Enforcement, p.Enforcement)
	}
	pr := ruleByType(p, "pull_request")
	if pr == nil {
		t.Fatal("payload has no pull_request rule")
	}
	assertInt(t, "required_approving_review_count", pr, float64(m.Rules.PullRequest.RequiredApprovals))
	assertBool(t, "dismiss_stale_reviews_on_push", pr, m.Rules.PullRequest.DismissStaleReviews)
	assertBool(t, "require_last_push_approval", pr, m.Rules.PullRequest.RequireLastPushApproval)
	assertBool(t, "required_review_thread_resolution", pr, m.Rules.PullRequest.RequireConversationResolution)
	assertBool(t, "require_code_owner_review", pr, m.Rules.PullRequest.RequireCodeOwnerReview)

	assertRulePresence(t, p, "required_linear_history", m.Rules.RequireLinearHistory)
	assertRulePresence(t, p, "required_signatures", m.Rules.RequireSignedCommits)
	assertRulePresence(t, p, "non_fast_forward", m.Rules.BlockForcePushes)
	assertRulePresence(t, p, "deletion", m.Rules.BlockDeletions)

	sc := ruleByType(p, "required_status_checks")
	if sc == nil {
		t.Fatal("payload has no required_status_checks rule")
	}
	if len(m.Rules.RequiredStatusChecks.Contexts) != 0 || len(statusCheckContexts(sc)) != 0 {
		t.Errorf("profile contexts must stay empty (repository CI derives them): governance %v payload %v",
			m.Rules.RequiredStatusChecks.Contexts, statusCheckContexts(sc))
	}
	if got := sc.Parameters["strict_required_status_checks_policy"]; got != m.Rules.RequiredStatusChecks.Strict {
		t.Errorf("strict_required_status_checks_policy: payload %v != governance %t", got, m.Rules.RequiredStatusChecks.Strict)
	}
}

func checkTagsAgainstGovernance(t *testing.T, gov *config.Governance, p *ghub.RulesetPayload) {
	t.Helper()
	m := gov.Rulesets.ReleaseTags
	if m.Enforcement != p.Enforcement {
		t.Errorf("tags enforcement: governance %q != payload %q", m.Enforcement, p.Enforcement)
	}
	if p.Conditions == nil || strings.Join(p.Conditions.RefName.Include, ",") != strings.Join(m.Patterns, ",") {
		got := "<nil>"
		if p.Conditions != nil {
			got = strings.Join(p.Conditions.RefName.Include, ",")
		}
		t.Errorf("tags conditions include = %q, governance patterns = %q", got, strings.Join(m.Patterns, ","))
	}
	assertRulePresence(t, p, "deletion", m.Rules.BlockDeletions)
	assertRulePresence(t, p, "update", m.Rules.BlockUpdates)
	// tag_name_pattern is an Enterprise-plan metadata restriction
	// ("Organizations on a GitHub Enterprise plan can access additional
	// rules to control how commit metadata must be formatted... You can
	// also control the format of new branch names and tag names",
	// docs.github.com "Available rules for rulesets"). The governance
	// document declares the intent; the canonical payload deliberately
	// does not realize it, so it must never appear there.
	if ruleByType(p, "tag_name_pattern") != nil {
		t.Error("tag_name_pattern is Enterprise-only and must stay out of the canonical payload")
	}
}

func ruleByType(p *ghub.RulesetPayload, typ string) *ghub.RulesetRule {
	for i := range p.Rules {
		if p.Rules[i].Type == typ {
			return &p.Rules[i]
		}
	}
	return nil
}

func assertRulePresence(t *testing.T, p *ghub.RulesetPayload, typ string, want bool) {
	t.Helper()
	if got := ruleByType(p, typ) != nil; got != want {
		t.Errorf("rule %q present = %t, governance wants %t", typ, got, want)
	}
}

func assertBool(t *testing.T, key string, r *ghub.RulesetRule, want bool) {
	t.Helper()
	got, ok := r.Parameters[key].(bool)
	if !ok {
		t.Errorf("pull_request parameter %q missing or not a bool", key)
		return
	}
	if got != want {
		t.Errorf("pull_request.%s = %t, governance wants %t", key, got, want)
	}
}

func assertInt(t *testing.T, key string, r *ghub.RulesetRule, want float64) {
	t.Helper()
	got, ok := r.Parameters[key].(float64)
	if !ok {
		t.Errorf("pull_request parameter %q missing or not a number", key)
		return
	}
	if got != want {
		t.Errorf("pull_request.%s = %v, governance wants %v", key, got, want)
	}
}

func statusCheckContexts(r *ghub.RulesetRule) []string {
	var out []string
	raw, ok := r.Parameters["required_status_checks"].([]any)
	if !ok {
		if typed, ok := r.Parameters["required_status_checks"].([]map[string]any); ok {
			for _, m := range typed {
				if c, ok := m["context"].(string); ok {
					out = append(out, c)
				}
			}
		}
		return out
	}
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			if c, ok := m["context"].(string); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func anyToStrings(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
