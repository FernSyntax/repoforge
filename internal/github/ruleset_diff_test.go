// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"encoding/json"
	"strings"
	"testing"
)

// conformingPayload mirrors the shape of the canonical RepoForge
// payloads (configs/github/rulesets/): the github package cannot
// import configs (configs imports github), so the fixture is local.
func conformingPayload() *RulesetPayload {
	return &RulesetPayload{
		Name:         "repoforge:main",
		Target:       "branch",
		Enforcement:  "active",
		BypassActors: []RulesetBypassActor{},
		Conditions: &RulesetConditions{
			RefName: RulesetRefName{Include: []string{"~DEFAULT_BRANCH"}, Exclude: []string{}},
		},
		Rules: []RulesetRule{
			{Type: "pull_request", Parameters: map[string]any{
				"allowed_merge_methods":             []any{"squash"},
				"dismiss_stale_reviews_on_push":     false,
				"require_code_owner_review":         false,
				"require_last_push_approval":        false,
				"required_approving_review_count":   float64(0),
				"required_review_thread_resolution": true,
			}},
			{Type: "required_status_checks", Parameters: map[string]any{
				"required_status_checks":               []any{map[string]any{"context": "verify"}},
				"strict_required_status_checks_policy": true,
			}},
			{Type: "required_linear_history"},
			{Type: "non_fast_forward"},
			{Type: "deletion"},
		},
	}
}

// detailOf round-trips a payload through JSON, exactly as the API
// would return it (same documented field names).
func detailOf(t *testing.T, p *RulesetPayload) *RulesetDetail {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var d RulesetDetail
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestRulesetDriftConformingIsSilent(t *testing.T) {
	want := conformingPayload()
	if reasons := RulesetDrift(detailOf(t, want), want); len(reasons) != 0 {
		t.Fatalf("conforming ruleset reported drift: %v", reasons)
	}
	if RulesetDrifted(detailOf(t, want), want) {
		t.Fatal("RulesetDrifted = true for conforming ruleset")
	}
}

func TestRulesetDriftDetectsEnforcement(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.Enforcement = "disabled"
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 || !strings.Contains(reasons[0], `enforcement is "disabled"`) {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestRulesetDriftDetectsMissingRule(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.Rules = existing.Rules[:len(existing.Rules)-1] // drop "deletion"
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 || !strings.Contains(reasons[0], `required rule "deletion" is missing`) {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestRulesetDriftDetectsExtraRule(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.Rules = append(existing.Rules, struct {
		Type       string          `json:"type"`
		Parameters json.RawMessage `json:"parameters"`
	}{Type: "creation"})
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 || !strings.Contains(reasons[0], `rule "creation" is not part of the desired policy`) {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestRulesetDriftDetectsPullRequestParamChange(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	// Simulate a hand edit: 2 required approvals instead of 0.
	params := existing.PullRequestRuleParams()
	params.RequiredApprovingReviewCount = 2
	for i := range existing.Rules {
		if existing.Rules[i].Type == "pull_request" {
			raw, _ := json.Marshal(params)
			existing.Rules[i].Parameters = raw
		}
	}
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "required_approving_review_count is 2, policy wants 0") {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestRulesetDriftDetectsStatusCheckChange(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	for i := range existing.Rules {
		if existing.Rules[i].Type == "required_status_checks" {
			existing.Rules[i].Parameters = json.RawMessage(
				`{"required_status_checks":[{"context":"other"}],"strict_required_status_checks_policy":false}`)
		}
	}
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 2 {
		t.Fatalf("reasons = %v", reasons)
	}
	if !strings.Contains(reasons[0], `contexts are [other], policy wants [verify]`) {
		t.Errorf("reasons[0] = %q", reasons[0])
	}
	if !strings.Contains(reasons[1], "strict policy is false, policy wants true") {
		t.Errorf("reasons[1] = %q", reasons[1])
	}
}

func TestRulesetDriftDetectsConditionChange(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.Conditions.RefName.Include = []string{"~ALL"}
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "conditions.ref_name.include is [~ALL], policy wants [~DEFAULT_BRANCH]") {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestRulesetDriftMissingConditions(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.Conditions = nil
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "ref conditions are missing") {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestRulesetDriftSkipsUnreportedBypassActors(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.BypassActors = nil // API omits the property for read-only callers
	if reasons := RulesetDrift(existing, want); len(reasons) != 0 {
		t.Fatalf("nil bypass_actors must not report drift: %v", reasons)
	}
}

func TestRulesetDriftDetectsReportedBypassActors(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	id := 42
	actors := []RulesetBypassActor{{ActorID: &id, ActorType: "RepositoryRole", BypassMode: "always"}}
	existing.BypassActors = &actors
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "bypass actors are [RepositoryRole/42/always], policy wants [none]") {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestRulesetDriftOrderIsStable(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.Enforcement = "disabled"
	existing.Rules = existing.Rules[:2] // drop required_status_checks, linear, non_fast_forward, deletion
	existing.Rules = append(existing.Rules, struct {
		Type       string          `json:"type"`
		Parameters json.RawMessage `json:"parameters"`
	}{Type: "creation"})

	first := RulesetDrift(existing, want)
	for i := 0; i < 5; i++ {
		again := RulesetDrift(existing, want)
		if strings.Join(first, "|") != strings.Join(again, "|") {
			t.Fatalf("drift reasons not stable:\n%v\n%v", first, again)
		}
	}
	if len(first) != 5 {
		t.Fatalf("expected enforcement + 3 missing + 1 extra, got %v", first)
	}
}

// --- conditions ------------------------------------------------------------

// TestRulesetDriftWithoutConditions proves conditions are optional:
// a payload that declares none must not report drift against a
// ruleset that happens to carry them — the policy simply does not
// manage ref scoping.
func TestRulesetDriftWithoutConditions(t *testing.T) {
	want := conformingPayload()
	want.Conditions = nil
	existing := detailOf(t, want)
	if reasons := RulesetDrift(existing, want); len(reasons) != 0 {
		t.Fatalf("a payload without conditions must not drift: %v", reasons)
	}
}

// TestRulesetDriftDetectsExcludeChange proves both halves of the ref
// condition are compared: excluding a ref is as much policy as
// including one, and a one-sided check would let an exclusion drift
// forever.
func TestRulesetDriftDetectsExcludeChange(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.Conditions.RefName.Include = []string{"~ALL"}
	existing.Conditions.RefName.Exclude = []string{"release/*"}
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 2 {
		t.Fatalf("reasons = %v, want the include and exclude changes", reasons)
	}
	if !strings.Contains(reasons[0], "include is [~ALL], policy wants [~DEFAULT_BRANCH]") {
		t.Errorf("reasons[0] = %q", reasons[0])
	}
	if !strings.Contains(reasons[1], "exclude is [release/*], policy wants []") {
		t.Errorf("reasons[1] = %q", reasons[1])
	}
}

// --- parameterized rules ---------------------------------------------------

// TestRulesetDriftReportsMissingParameters proves a rule present but
// stripped of its parameters is drift, not silence: the comparison
// would otherwise pass a ruleset that cannot enforce anything.
func TestRulesetDriftReportsMissingParameters(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.Rules[0].Parameters = nil // pull_request without parameters
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 || !strings.Contains(reasons[0], `rule "pull_request" has no parameters`) {
		t.Fatalf("reasons = %v", reasons)
	}
	existing = detailOf(t, want)
	existing.Rules[1].Parameters = nil // required_status_checks without parameters
	reasons = RulesetDrift(existing, want)
	if len(reasons) != 1 || !strings.Contains(reasons[0], `rule "required_status_checks" has no parameters`) {
		t.Fatalf("reasons = %v", reasons)
	}
}

// TestRulesetDriftDetectsPullRequestBoolChange proves the boolean
// parameters RepoForge sets are each compared: a hand edit to any one
// of them is drift, and comparing only the review count would miss a
// protection being quietly turned off.
func TestRulesetDriftDetectsPullRequestBoolChange(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.Rules[0].Parameters = json.RawMessage(`{
		"allowed_merge_methods": ["squash", "merge"],
		"required_review_thread_resolution": false,
		"dismiss_stale_reviews_on_push": true,
		"required_approving_review_count": 0
	}`)
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 3 {
		t.Fatalf("reasons = %v, want the two booleans and the merge methods", reasons)
	}
	for _, w := range []string{
		"pull_request.dismiss_stale_reviews_on_push is true, policy wants false",
		"pull_request.required_review_thread_resolution is false, policy wants true",
		"pull_request.allowed_merge_methods is [squash, merge], policy wants [squash]",
	} {
		found := false
		for _, r := range reasons {
			if strings.Contains(r, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing reason %q in %v", w, reasons)
		}
	}
}

// TestRulesetDriftAcceptsOverlayParameterShapes proves the want side
// accepts both shapes a parameter can arrive in: []map[string]any
// from the desired-rulesets overlay and a plain int, not only the
// float64 JSON decoding yields — otherwise an overlay-configured
// policy would drift against itself.
func TestRulesetDriftAcceptsOverlayParameterShapes(t *testing.T) {
	want := conformingPayload()
	want.Rules[0].Parameters["allowed_merge_methods"] = []string{"squash"}
	want.Rules[0].Parameters["required_approving_review_count"] = 0
	want.Rules[1].Parameters["required_status_checks"] = []map[string]any{{"context": "verify"}}
	existing := detailOf(t, want)
	if reasons := RulesetDrift(existing, want); len(reasons) != 0 {
		t.Fatalf("the overlay shapes must compare equal: %v", reasons)
	}
}

// TestRulesetDriftSkipsUndeclaredContexts proves a policy that
// declares no status-check contexts is not misread as "zero
// contexts": the key is simply absent from the payload, and drift
// against an absent parameter would be unfixable by any payload
// change.
func TestRulesetDriftSkipsUndeclaredContexts(t *testing.T) {
	want := conformingPayload()
	delete(want.Rules[1].Parameters, "required_status_checks")
	existing := detailOf(t, want)
	if reasons := RulesetDrift(existing, want); len(reasons) != 0 {
		t.Fatalf("an absent context list must be skipped: %v", reasons)
	}
}

// TestRulesetDriftIgnoresUnusableParameterValues proves a parameter
// the policy cannot express is skipped rather than misread: a
// non-numeric review count is not "zero", and treating it as such
// would report drift that no payload change can fix. The existing
// side stays a clean payload — a corrupt existing is a corrupt
// repository, not a policy question.
func TestRulesetDriftIgnoresUnusableParameterValues(t *testing.T) {
	existing := detailOf(t, conformingPayload())
	want := conformingPayload()
	want.Rules[0].Parameters["required_approving_review_count"] = "two"
	if reasons := RulesetDrift(existing, want); len(reasons) != 0 {
		t.Fatalf("an unexpressible parameter must be skipped: %v", reasons)
	}
}

// TestRulesetDriftReportsUnusableMergeMethodList proves a non-list
// allowed_merge_methods cannot compare equal: the existing methods
// are real, the policy's value decodes to nothing, and silence here
// would hide a policy that cannot be applied. Unlike the review
// count, this parameter cannot be skipped: an empty want list is a
// real (drifted) state, not an unreadable one. The existing side
// stays a clean payload — a corrupt existing is a corrupt
// repository, not a policy question.
func TestRulesetDriftReportsUnusableMergeMethodList(t *testing.T) {
	existing := detailOf(t, conformingPayload())
	want := conformingPayload()
	want.Rules[0].Parameters["allowed_merge_methods"] = "squash"
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 ||
		!strings.Contains(reasons[0], "allowed_merge_methods is [squash], policy wants []") {
		t.Fatalf("reasons = %v", reasons)
	}
}

// --- bypass actors ---------------------------------------------------------

// TestRulesetDriftReportsUnknownBypassActorShape proves the bypass
// comparison covers actor shapes the payload does not manage: a
// DeployKey actor (no id, no mode) must still be named, using the
// documented defaults, or a hand-added bypass would go unreported.
func TestRulesetDriftReportsUnknownBypassActorShape(t *testing.T) {
	want := conformingPayload()
	existing := detailOf(t, want)
	existing.BypassActors = &[]RulesetBypassActor{{ActorType: "DeployKey"}}
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 ||
		!strings.Contains(reasons[0], "bypass actors are [DeployKey/nil/always]") {
		t.Fatalf("reasons = %v", reasons)
	}
}

// TestRulesetDriftComparesBypassActorsOfEqualCount proves the
// comparison is by content, not by count: swapping one actor for
// another is drift even though both sides carry exactly one.
func TestRulesetDriftComparesBypassActorsOfEqualCount(t *testing.T) {
	want := conformingPayload()
	want.BypassActors = []RulesetBypassActor{
		{ActorID: intPtr(43), ActorType: "RepositoryRole", BypassMode: "always"},
	}
	existing := detailOf(t, want)
	id := 42
	existing.BypassActors = &[]RulesetBypassActor{
		{ActorID: &id, ActorType: "RepositoryRole", BypassMode: "always"},
	}
	reasons := RulesetDrift(existing, want)
	if len(reasons) != 1 ||
		!strings.Contains(reasons[0], "bypass actors are [RepositoryRole/42/always], policy wants [RepositoryRole/43/always]") {
		t.Fatalf("reasons = %v", reasons)
	}
}

func intPtr(n int) *int { return &n }
