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
