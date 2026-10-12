// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// This file implements desired-vs-actual comparison for
// RepoForge-owned rulesets. It is pure (no I/O): bootstrap uses it
// to decide create/update/no-op (spec.md §10.3) and doctor uses it to
// report drift (spec.md §11.3).

// StatusCheckRuleParams are the documented parameters of the
// required_status_checks ruleset rule.
type StatusCheckRuleParams struct {
	RequiredStatusChecks []struct {
		Context string `json:"context"`
	} `json:"required_status_checks"`
	StrictRequiredStatusChecksPolicy bool `json:"strict_required_status_checks_policy"`
}

// StatusCheckRuleParams returns the decoded required_status_checks
// rule parameters, or nil if the rule is absent or has no parameters.
func (d *RulesetDetail) StatusCheckRuleParams() *StatusCheckRuleParams {
	for _, r := range d.Rules {
		if r.Type == "required_status_checks" && len(r.Parameters) > 0 {
			var p StatusCheckRuleParams
			if err := json.Unmarshal(r.Parameters, &p); err == nil {
				return &p
			}
		}
	}
	return nil
}

// RulesetDrift reports the differences between an existing ruleset and
// the desired payload over the complete RepoForge-owned state:
// enforcement, ref conditions, the rule-type set, the managed
// parameters of the parameterized rules, and bypass actors when the
// API reports them.
//
// Ownership is total (spec.md §5.2): a rule present remotely but absent in
// the payload is drift, because the next update replaces the rule set
// wholesale. Conversely, documented API defaults *within* a rule
// (e.g. do_not_enforce_on_create, dismissal_restriction) are not
// compared — only the fields this tool sets — so a conforming ruleset
// never reports perpetual drift (spec.md §10.2).
//
// Bypass actors are skipped when the API omits the property: it is
// only returned to callers with write access to the ruleset
// (documented), so a read-only audit cannot classify it (spec.md §7.4).
//
// The returned reasons are stable: enforcement, conditions, wanted
// rules in payload order, extra rules sorted by type, bypass actors.
func RulesetDrift(existing *RulesetDetail, want *RulesetPayload) []string {
	var reasons []string

	if existing.Enforcement != want.Enforcement {
		reasons = append(reasons, fmt.Sprintf("enforcement is %q, policy wants %q",
			existing.Enforcement, want.Enforcement))
	}

	reasons = append(reasons, conditionsDrift(existing, want)...)

	wantRules := make(map[string]*RulesetRule, len(want.Rules))
	for i := range want.Rules {
		wantRules[want.Rules[i].Type] = &want.Rules[i]
	}
	existingRules := make(map[string]bool, len(existing.Rules))
	for _, r := range existing.Rules {
		existingRules[r.Type] = true
	}

	for i := range want.Rules {
		wr := &want.Rules[i]
		if !existingRules[wr.Type] {
			reasons = append(reasons, fmt.Sprintf("required rule %q is missing", wr.Type))
			continue
		}
		switch wr.Type {
		case "pull_request":
			reasons = append(reasons, pullRequestDrift(existing, wr)...)
		case "required_status_checks":
			reasons = append(reasons, statusCheckDrift(existing, wr)...)
		}
	}

	var extra []string
	for typ := range existingRules {
		if _, ok := wantRules[typ]; !ok {
			extra = append(extra, typ)
		}
	}
	sort.Strings(extra)
	for _, typ := range extra {
		reasons = append(reasons, fmt.Sprintf("rule %q is not part of the desired policy", typ))
	}

	reasons = append(reasons, bypassDrift(existing, want)...)
	return reasons
}

// RulesetDrifted reports whether any managed state differs.
func RulesetDrifted(existing *RulesetDetail, want *RulesetPayload) bool {
	return len(RulesetDrift(existing, want)) > 0
}

func conditionsDrift(existing *RulesetDetail, want *RulesetPayload) []string {
	if want.Conditions == nil {
		return nil
	}
	if existing.Conditions == nil {
		return []string{"ref conditions are missing"}
	}
	var reasons []string
	if !sameStrings(existing.Conditions.RefName.Include, want.Conditions.RefName.Include) {
		reasons = append(reasons, fmt.Sprintf("conditions.ref_name.include is %s, policy wants %s",
			formatList(existing.Conditions.RefName.Include), formatList(want.Conditions.RefName.Include)))
	}
	if !sameStrings(existing.Conditions.RefName.Exclude, want.Conditions.RefName.Exclude) {
		reasons = append(reasons, fmt.Sprintf("conditions.ref_name.exclude is %s, policy wants %s",
			formatList(existing.Conditions.RefName.Exclude), formatList(want.Conditions.RefName.Exclude)))
	}
	return reasons
}

// pullRequestDrift compares only the parameters RepoForge sets.
// The remaining documented parameters (dismissal_restriction,
// required_reviewers) are not part of the policy and are left alone.
func pullRequestDrift(existing *RulesetDetail, want *RulesetRule) []string {
	params := existing.PullRequestRuleParams()
	if params == nil {
		return []string{`rule "pull_request" has no parameters`}
	}
	var reasons []string
	compareBool := func(key string, gotV bool) {
		wantV, ok := want.Parameters[key].(bool)
		if ok && gotV != wantV {
			reasons = append(reasons, fmt.Sprintf("pull_request.%s is %t, policy wants %t", key, gotV, wantV))
		}
	}
	compareBool("dismiss_stale_reviews_on_push", params.DismissStaleReviewsOnPush)
	compareBool("require_code_owner_review", params.RequireCodeOwnerReview)
	compareBool("require_last_push_approval", params.RequireLastPushApproval)
	compareBool("required_review_thread_resolution", params.RequiredReviewThreadResolution)
	if wantV, ok := want.Parameters["required_approving_review_count"]; ok {
		if wantN := intParam(wantV); wantN >= 0 && params.RequiredApprovingReviewCount != wantN {
			reasons = append(reasons, fmt.Sprintf("pull_request.required_approving_review_count is %d, policy wants %d",
				params.RequiredApprovingReviewCount, wantN))
		}
	}
	if wantV, ok := want.Parameters["allowed_merge_methods"]; ok {
		wantMethods := anyToStrings(wantV)
		if !sameStrings(params.AllowedMergeMethods, wantMethods) {
			reasons = append(reasons, fmt.Sprintf("pull_request.allowed_merge_methods is %s, policy wants %s",
				formatList(params.AllowedMergeMethods), formatList(wantMethods)))
		}
	}
	return reasons
}

func statusCheckDrift(existing *RulesetDetail, want *RulesetRule) []string {
	params := existing.StatusCheckRuleParams()
	if params == nil {
		return []string{`rule "required_status_checks" has no parameters`}
	}
	var reasons []string
	var wantContexts []string
	for _, item := range anyToSlice(want.Parameters["required_status_checks"]) {
		if m, ok := item.(map[string]any); ok {
			if ctx, ok := m["context"].(string); ok {
				wantContexts = append(wantContexts, ctx)
			}
		}
	}
	var gotContexts []string
	for _, c := range params.RequiredStatusChecks {
		gotContexts = append(gotContexts, c.Context)
	}
	if !sameStrings(gotContexts, wantContexts) {
		reasons = append(reasons, fmt.Sprintf("required_status_checks contexts are %s, policy wants %s",
			formatList(gotContexts), formatList(wantContexts)))
	}
	if wantV, ok := want.Parameters["strict_required_status_checks_policy"].(bool); ok &&
		params.StrictRequiredStatusChecksPolicy != wantV {
		reasons = append(reasons, fmt.Sprintf("required_status_checks strict policy is %t, policy wants %t",
			params.StrictRequiredStatusChecksPolicy, wantV))
	}
	return reasons
}

// bypassDrift compares bypass actors only when the API returned them
// (a read-only caller never sees the property). RepoForge payloads
// always carry an empty list: nobody bypasses the ruleset.
func bypassDrift(existing *RulesetDetail, want *RulesetPayload) []string {
	if existing.BypassActors == nil {
		return nil
	}
	got := *existing.BypassActors
	if len(got) == 0 && len(want.BypassActors) == 0 {
		return nil
	}
	if sameBypassActors(got, want.BypassActors) {
		return nil
	}
	return []string{fmt.Sprintf("bypass actors are %s, policy wants %s",
		formatBypass(got), formatBypass(want.BypassActors))}
}

func sameBypassActors(a, b []RulesetBypassActor) bool {
	if len(a) != len(b) {
		return false
	}
	as := formatBypass(a)
	bs := formatBypass(b)
	return as == bs
}

func formatBypass(actors []RulesetBypassActor) string {
	if len(actors) == 0 {
		return "[none]"
	}
	parts := make([]string, 0, len(actors))
	for _, a := range actors {
		id := "nil"
		if a.ActorID != nil {
			id = fmt.Sprintf("%d", *a.ActorID)
		}
		mode := a.BypassMode
		if mode == "" {
			mode = "always"
		}
		parts = append(parts, fmt.Sprintf("%s/%s/%s", a.ActorType, id, mode))
	}
	sort.Strings(parts)
	return "[" + strings.Join(parts, " ") + "]"
}

func formatList(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	return "[" + strings.Join(v, ", ") + "]"
}

// intParam decodes a JSON number held as any (encoding/json yields
// float64). Returns -1 when the value is not a number.
func intParam(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return -1
}

// anyToSlice normalizes the two shapes a parameter list can take:
// []any from JSON decoding and []map[string]any from the DesiredRulesets
// overlay.
func anyToSlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []map[string]any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = t[i]
		}
		return out
	}
	return nil
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

// sameStrings compares as multisets (order-insensitive): the API may
// reorder arrays without changing meaning.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
