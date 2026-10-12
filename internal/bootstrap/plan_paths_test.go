// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/configs"
	"github.com/FernSyntax/repoforge/internal/config"
	"github.com/FernSyntax/repoforge/internal/doctor"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// These tests drive computePlan and its helpers (planLabels,
// planRulesets, planSecurity) with synthetic desired and actual state.
// Reaching them through Run would need one fixture repository per
// branch, and the branch is what matters: the plan is the whole safety
// contract, so every difference shape — missing rule, extra rule,
// changed parameter, converged — is asserted as an exact (action,
// detail) pair rather than as "some change was planned".

// planCase is one cell of the plan matrix.
type planCase struct {
	name string
	// setup writes the remote state the plan reconciles against. It
	// runs before the desired state is read from disk, because some
	// seeds derive their state from the same policy the plan will use.
	setup func(*testing.T, *fakeGitHub, string)
	// mutate edits the desired state after it is loaded from the
	// fixture. Nil keeps the fixture's policy and governance document
	// verbatim.
	mutate func(*config.Policy, *config.Governance)
	// area and subject identify the single plan item under assertion.
	area, subject string
	wantAction    Action
	wantDetail    string
	// contextlessRoot plans against a repository with no CI that
	// reports on pull requests, instead of the standard fixture.
	contextlessRoot bool
}

// planPolicies covers every governance profile: the same branch is
// reached with a different desired ruleset for each, so a defect that
// only shows on one profile is not hidden by the others.
var planPolicies = []string{"solo", "team", "strict"}

func TestComputePlanMatrix(t *testing.T) {
	cases := []planCase{
		// --- repository metadata ------------------------------------
		{
			name: "visibility drift is report-only",
			area: "repository", subject: "visibility",
			wantAction: ActionReport,
			wantDetail: `is "public", policy wants "internal"; visibility changes are never automated (spec.md §8.5) — change manually`,
			mutate:     func(p *config.Policy, _ *config.Governance) { p.Repository.Visibility = "internal" },
		},
		{
			name: "visibility matches",
			area: "repository", subject: "visibility",
			wantAction: ActionNoop, wantDetail: "is public",
		},
		{
			// The schema default is public, so a policy that omits
			// visibility must compare against public, not against "".
			name: "unset policy visibility defaults to public",
			area: "repository", subject: "visibility",
			wantAction: ActionNoop, wantDetail: "is public",
			mutate: func(p *config.Policy, _ *config.Governance) { p.Repository.Visibility = "" },
		},

		// --- repository settings ------------------------------------
		{
			name: "drifted settings are one update item",
			area: "repository", subject: "settings",
			wantAction: ActionUpdate,
			wantDetail: "update: allow_merge_commit, allow_rebase_merge, delete_branch_on_merge",
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.GitHub.Projects = true
				p.Merge.MergeCommit = false
				p.Merge.Rebase = false
				p.Merge.DeleteBranch = boolPtr(true)
			},
		},
		{
			name: "converged settings are a no-op",
			area: "repository", subject: "settings",
			wantAction: ActionNoop, wantDetail: "merge and feature settings match policy",
			// The fixture policy and the fake both declare the
			// feature set; converging them means the remote carries
			// the same flags, not just that the policy is internally
			// consistent.
			setup: func(_ *testing.T, f *fakeGitHub, _ string) {
				f.features["has_discussions"] = true
			},
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.GitHub.Discussions = true
				p.GitHub.Projects = true
				p.GitHub.Wiki = true
				p.Merge.MergeCommit = true
				p.Merge.Rebase = true
				p.Merge.DeleteBranch = boolPtr(false)
				p.Merge.AutoMerge = false
			},
		},

		// --- topics -------------------------------------------------
		{
			name: "managed topics drift",
			area: "repository", subject: "topics",
			wantAction: ActionUpdate,
			wantDetail: "replace [] with [cli go]",
			mutate:     func(_ *config.Policy, g *config.Governance) { g.Repository.Topics = []string{"go", "cli"} },
		},
		{
			// Set comparison, not order comparison: the fake returns
			// the topics in the opposite order from the policy.
			name: "managed topics match in any order",
			area: "repository", subject: "topics",
			wantAction: ActionNoop, wantDetail: "topics match policy",
			setup:  func(_ *testing.T, f *fakeGitHub, _ string) { f.topics = []string{"cli", "go"} },
			mutate: func(_ *config.Policy, g *config.Governance) { g.Repository.Topics = []string{"go", "cli"} },
		},
		{
			name: "unmanaged topics are left untouched",
			area: "repository", subject: "topics",
			wantAction: ActionReport,
			wantDetail: "not policy-managed; 2 topic(s) present, left untouched",
			setup:      func(_ *testing.T, f *fakeGitHub, _ string) { f.topics = []string{"go", "cli"} },
		},

		// --- default branch -----------------------------------------
		{
			name: "default branch drift is a rename",
			area: "repository", subject: "default branch",
			wantAction: ActionRename,
			wantDetail: `rename "develop" to "main" (open PRs are retargeted by GitHub)`,
			setup:      func(_ *testing.T, f *fakeGitHub, _ string) { f.defaultBranch = "develop" },
		},
		{
			name: "default branch matches",
			area: "repository", subject: "default branch",
			wantAction: ActionNoop, wantDetail: "is main",
		},
		{
			name: "unset policy default branch defaults to main",
			area: "repository", subject: "default branch",
			wantAction: ActionNoop, wantDetail: "is main",
			mutate: func(p *config.Policy, _ *config.Governance) { p.Repository.DefaultBranch = "" },
		},

		// --- rulesets -----------------------------------------------
		{
			name: "missing owned rulesets are created",
			area: "rulesets", subject: "ruleset " + configs.MainRulesetName,
			wantAction: ActionCreate,
			wantDetail: "create branch ruleset (enforcement active, 5 rules)",
		},
		{
			name: "converged owned ruleset is a no-op",
			area: "rulesets", subject: "ruleset " + configs.MainRulesetName,
			wantAction: ActionNoop, wantDetail: "matches governance profile",
			setup: seedConformingRulesets,
		},
		{
			// The baseline: the owned rulesets are already conforming,
			// so the only ruleset item left is the unmanaged one.
			name: "unmanaged repository ruleset is a conflict",
			area: "rulesets", subject: "ruleset custom-gate",
			wantAction: ActionConflict,
			wantDetail: "unmanaged branch ruleset (enforcement evaluate) exists; left untouched — review manually",
			setup: func(t *testing.T, f *fakeGitHub, dir string) {
				seedConformingRulesets(t, f, dir)
				f.rulesets[7] = &fakeRuleset{id: 7, name: "custom-gate", target: "branch",
					sourceType: "Repository", enforcement: "evaluate"}
			},
		},
		{
			name: "inherited ruleset is reported read-only",
			area: "rulesets", subject: "ruleset org-policy",
			wantAction: ActionReport,
			wantDetail: "inherited from Organization (target branch); read-only at repository level",
			setup: func(t *testing.T, f *fakeGitHub, dir string) {
				seedConformingRulesets(t, f, dir)
				f.rulesets[8] = &fakeRuleset{id: 8, name: "org-policy", target: "branch",
					sourceType: "Organization", enforcement: "active"}
			},
		},
		{
			// Ownership is (target, name) together, not name alone:
			// the main ruleset name on a tag ruleset is somebody
			// else's object and must not be overwritten.
			name: "owned name on the wrong target is a conflict",
			area: "rulesets", subject: "ruleset " + configs.MainRulesetName,
			wantAction: ActionConflict,
			wantDetail: "unmanaged tag ruleset (enforcement active) exists; left untouched — review manually",
			setup: func(t *testing.T, f *fakeGitHub, dir string) {
				seedConformingRulesets(t, f, dir)
				f.rulesets[9] = &fakeRuleset{id: 9, name: configs.MainRulesetName, target: "tag",
					sourceType: "Repository", enforcement: "active"}
			},
		},
		{
			// A repository with no CI that reports on pull requests
			// cannot require status checks: requiring them would block
			// every merge forever.
			name: "no CI contexts discovered omits the status check rule",
			area: "rulesets", subject: "status checks",
			wantAction:      ActionReport,
			wantDetail:      "no CI job contexts discovered; required_status_checks rule omitted",
			contextlessRoot: true,
		},
		{
			name: "unknown governance profile cannot compose rulesets",
			area: "rulesets", subject: "rulesets",
			wantAction: ActionReport,
			wantDetail: `cannot compose desired rulesets: configs: unknown governance profile "nonesuch"`,
			mutate:     func(_ *config.Policy, g *config.Governance) { g.Profile = "nonesuch" },
		},

		// --- labels -------------------------------------------------
		{
			name: "missing canonical labels are created",
			area: "labels", subject: "label type:bug",
			wantAction: ActionCreate,
			wantDetail: "create label (color d73a4a)",
			setup:      seedNoLabels,
		},
		{
			name: "a drifted canonical label color is updated",
			area: "labels", subject: "label type:bug",
			wantAction: ActionUpdate,
			wantDetail: `color "000000" → "d73a4a", description "Something isn't working" → "Something isn't working"`,
			setup: func(t *testing.T, f *fakeGitHub, _ string) {
				seedCanonicalLabels(t, f)
				f.labels["type:bug"].color = "000000"
			},
		},
		{
			name: "a drifted canonical label description is updated",
			area: "labels", subject: "label priority:p0",
			wantAction: ActionUpdate,
			wantDetail: `color "d93f0b" → "d93f0b", description "edited by hand" → "Critical: drop everything"`,
			setup: func(t *testing.T, f *fakeGitHub, _ string) {
				seedCanonicalLabels(t, f)
				f.labels["priority:p0"].description = "edited by hand"
			},
		},
		{
			// Color comparison is normalized: a '#' prefix or a case
			// difference is not drift and must not cause a PATCH —
			// GitHub's API returns the hash form, the policy declares
			// the bare form.
			name: "a color differing only by hash or case is not drift",
			area: "labels", subject: "labels",
			wantAction: ActionNoop, wantDetail: "19 canonical labels match policy",
			setup: func(t *testing.T, f *fakeGitHub, _ string) {
				seedCanonicalLabels(t, f)
				f.labels["type:bug"].color = "#D73A4A"
			},
		},
		{
			// The same normalization covers descriptions: surrounding
			// whitespace is not semantic, so a description that
			// differs only by it matches and does not PATCH.
			name: "trailing whitespace in a description is not drift",
			area: "labels", subject: "labels",
			wantAction: ActionNoop, wantDetail: "19 canonical labels match policy",
			setup: func(t *testing.T, f *fakeGitHub, _ string) {
				seedCanonicalLabels(t, f)
				f.labels["type:bug"].description = "Something isn't working "
			},
		},
		{
			name: "converged canonical labels are a no-op",
			area: "labels", subject: "labels",
			wantAction: ActionNoop, wantDetail: "19 canonical labels match policy",
			setup: seedCanonicalLabelsInto,
		},
		{
			// User-owned labels are never modified and never deleted:
			// they are counted and reported instead.
			name: "unrelated labels are reported and left untouched",
			area: "labels", subject: "labels",
			wantAction: ActionReport,
			wantDetail: "2 unrelated label(s) present, left untouched",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) {
				f.labels["my-team"] = &fakeLabel{name: "my-team", color: "123456"}
				f.labels["hand-made"] = &fakeLabel{name: "hand-made", color: "abcdef"}
			},
		},

		// --- security: simple toggles -------------------------------
		{
			name: "dependabot alerts are enabled",
			area: "security", subject: "Dependabot alerts",
			wantAction: ActionEnable, wantDetail: "disabled → enabled",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) { f.vulnAlerts = false },
		},
		{
			name: "dependabot alerts are disabled",
			area: "security", subject: "Dependabot alerts",
			wantAction: ActionDisable, wantDetail: "enabled → disabled",
			setup:  func(_ *testing.T, f *fakeGitHub, _ string) { f.vulnAlerts = true },
			mutate: func(p *config.Policy, _ *config.Governance) { p.Security.Dependabot = boolPtr(false) },
		},
		{
			name: "dependabot alerts already match",
			area: "security", subject: "Dependabot alerts",
			wantAction: ActionNoop, wantDetail: "enabled",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) { f.vulnAlerts = true },
		},
		{
			name: "dependabot security updates are enabled",
			area: "security", subject: "Dependabot security updates",
			wantAction: ActionEnable, wantDetail: "disabled → enabled",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) { f.autoFixes = false },
		},
		{
			name: "dependabot security updates are disabled",
			area: "security", subject: "Dependabot security updates",
			wantAction: ActionDisable, wantDetail: "enabled → disabled",
			setup:  func(_ *testing.T, f *fakeGitHub, _ string) { f.autoFixes = true },
			mutate: func(p *config.Policy, _ *config.Governance) { p.Security.Dependabot = boolPtr(false) },
		},
		{
			name: "private vulnerability reporting is enabled",
			area: "security", subject: "Private vulnerability reporting",
			wantAction: ActionEnable, wantDetail: "disabled → enabled",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) { f.privateVulnReporting = false },
		},
		{
			name: "private vulnerability reporting is disabled",
			area: "security", subject: "Private vulnerability reporting",
			wantAction: ActionDisable, wantDetail: "enabled → disabled",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) { f.privateVulnReporting = true },
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.Security.PrivateVulnerabilityReporting = boolPtr(false)
			},
		},

		// --- security: secret scanning pair -------------------------
		{
			// Both features are one API object, so enabling either
			// sends the whole desired pair.
			name: "both scanning features are enabled together",
			area: "security", subject: "secret scanning",
			wantAction: ActionEnable,
			wantDetail: "secret scanning disabled → enabled, push protection disabled → enabled (applied together: they share one API object)",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) {
				f.secretScanning = "disabled"
				f.pushProtection = "disabled"
			},
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.Security.SecretScanning = boolPtr(true)
				p.Security.SecretScanningPushProtection = boolPtr(true)
			},
		},
		{
			// A disable is always reachable: GitHub permits it on any
			// plan, and the pair must go out together so the feature
			// the policy does keep is not re-enabled.
			name: "both scanning features are disabled together",
			area: "security", subject: "secret scanning",
			wantAction: ActionDisable,
			wantDetail: "secret scanning enabled → disabled, push protection enabled → disabled (applied together: they share one API object)",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) {
				f.secretScanning = "enabled"
				f.pushProtection = "enabled"
			},
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.Security.SecretScanning = boolPtr(false)
				p.Security.SecretScanningPushProtection = boolPtr(false)
			},
		},
		{
			name: "both scanning features already match",
			area: "security", subject: "secret scanning",
			wantAction: ActionNoop, wantDetail: "enabled",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) {
				f.secretScanning = "enabled"
				f.pushProtection = "enabled"
			},
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.Security.SecretScanning = boolPtr(true)
				p.Security.SecretScanningPushProtection = boolPtr(true)
			},
		},
		{
			// Scanning is turned off while push protection stays on:
			// the single mutation still carries the full desired pair.
			name: "disabling scanning alone carries the whole desired pair",
			area: "security", subject: "secret scanning",
			wantAction: ActionDisable,
			wantDetail: "secret scanning enabled → disabled, push protection enabled → enabled (applied together: they share one API object)",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) {
				f.secretScanning = "enabled"
				f.pushProtection = "enabled"
			},
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.Security.SecretScanning = boolPtr(false)
				p.Security.SecretScanningPushProtection = boolPtr(true)
			},
		},
		{
			// Enabling one feature of the pair still takes the enable
			// branch: the plan classifies the transition, not the
			// number of fields that moved.
			name: "push protection moves while scanning is already enabled",
			area: "security", subject: "secret scanning",
			wantAction: ActionEnable,
			wantDetail: "secret scanning enabled → enabled, push protection disabled → enabled (applied together: they share one API object)",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) {
				f.secretScanning = "enabled"
				f.pushProtection = "disabled"
			},
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.Security.SecretScanning = boolPtr(true)
				p.Security.SecretScanningPushProtection = boolPtr(true)
			},
		},
		{
			// Enabling on a private repository needs a GitHub
			// Advanced Security product, so the transition can never
			// converge: it is reported, never attempted.
			name: "enabling on a private repository is unsupported",
			area: "security", subject: "secret scanning",
			wantAction: ActionUnsupported,
			wantDetail: "disabled; enabling requires a GitHub Advanced Security product on this private repository — set security policy explicitly or upgrade",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) {
				f.visibility = "private"
				f.secretScanning = "disabled"
				f.pushProtection = "disabled"
			},
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.Repository.Visibility = "private"
				p.Security.SecretScanning = boolPtr(true)
				p.Security.SecretScanningPushProtection = boolPtr(true)
			},
		},

		// --- security: code scanning ---------------------------------
		{
			name: "code scanning is configured on a public repository",
			area: "security", subject: "code scanning",
			wantAction: ActionEnable, wantDetail: "configure CodeQL default setup (go)",
			mutate: func(p *config.Policy, _ *config.Governance) { p.Security.CodeScanning = true },
		},
		{
			name: "code scanning already configured",
			area: "security", subject: "code scanning",
			wantAction: ActionNoop, wantDetail: "enabled",
			setup:  func(_ *testing.T, f *fakeGitHub, _ string) { f.codeScanningSetup = "configured" },
			mutate: func(p *config.Policy, _ *config.Governance) { p.Security.CodeScanning = true },
		},
		{
			name: "code scanning on a private repository needs a product",
			area: "security", subject: "code scanning",
			wantAction: ActionUnsupported,
			wantDetail: "not configured; private repositories require GitHub Code Security",
			setup: func(_ *testing.T, f *fakeGitHub, _ string) {
				f.visibility = "private"
				f.secretScanning = "enabled"
				f.pushProtection = "enabled"
			},
			mutate: func(p *config.Policy, _ *config.Governance) {
				p.Repository.Visibility = "private"
				p.Security.CodeScanning = true
				p.Security.SecretScanning = boolPtr(true)
				p.Security.SecretScanningPushProtection = boolPtr(true)
			},
		},
		{
			name: "code scanning without a supported language is unsupported",
			area: "security", subject: "code scanning",
			wantAction: ActionUnsupported,
			wantDetail: "no CodeQL-supported languages detected",
			setup:      func(_ *testing.T, f *fakeGitHub, _ string) { f.languages = map[string]int{"Shell": 10} },
			mutate:     func(p *config.Policy, _ *config.Governance) { p.Security.CodeScanning = true },
		},
		{
			// Security features are never silently switched off: a
			// configured scan the policy does not ask for is reported.
			name: "code scanning configured but not required is left alone",
			area: "security", subject: "code scanning",
			wantAction: ActionReport,
			wantDetail: "configured but policy does not require it; left enabled (never silently disable security features)",
			setup:      func(_ *testing.T, f *fakeGitHub, _ string) { f.codeScanningSetup = "configured" },
		},

		// --- security: dependency review -----------------------------
		{
			name: "dependency review is enforced in CI only",
			area: "security", subject: "dependency review",
			wantAction: ActionReport,
			wantDetail: "enforced in CI via dependency-review-action; no repository API to manage it",
		},
		{
			name: "dependency review not required",
			area: "security", subject: "dependency review",
			wantAction: ActionNoop, wantDetail: "not required by policy",
			mutate: func(p *config.Policy, _ *config.Governance) { p.Security.DependencyReview = boolPtr(false) },
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := initRepo(t, bootstrapPolicy)
			if c.contextlessRoot {
				removeWorkflows(t, dir)
			}
			f := newFakeGitHub(t)
			if c.setup != nil {
				c.setup(t, f, dir)
			}
			pol, gov, err := planDesired(t, dir)
			if err != nil {
				t.Fatalf("loading the desired state: %v", err)
			}
			if c.mutate != nil {
				c.mutate(pol, gov)
			}
			plan := computePlan(dir, pol, gov, readFake(t, f), &execEnv{owner: "o", repo: "r"})
			item, ok := findItem(plan, c.area, c.subject)
			if !ok {
				t.Fatalf("no plan item for %s/%s in %v", c.area, c.subject, planItems(plan))
			}
			if item.Action != c.wantAction {
				t.Errorf("action = %s, want %s — %s", item.Action, c.wantAction, item.Detail)
			}
			if item.Detail != c.wantDetail {
				t.Errorf("detail =\n  %q\nwant\n  %q", item.Detail, c.wantDetail)
			}
		})
	}
}

// TestPlanCreateItemsAreOrdered pins the one ordering guarantee the
// plan gives: items are applied in the order they were appended, so a
// caller that displays or applies them in order is not surprised by a
// re-ordering change. Rulesets are created before labels because a
// merge must be gated by the checks before it is gated by the labels.
func TestPlanCreateItemsAreOrdered(t *testing.T) {
	for _, profile := range planPolicies {
		t.Run(profile, func(t *testing.T) {
			dir := initRepo(t, lifecyclePolicy(profile))
			f := newFakeGitHub(t)
			// No labels are seeded: the ordering under assertion is
			// the order of creates, and a label that already exists
			// is not a create.
			pol, gov, err := planDesired(t, dir)
			if err != nil {
				t.Fatal(err)
			}
			plan := computePlan(dir, pol, gov, readFake(t, f), &execEnv{owner: "o", repo: "r"})

			var subjects []string
			for _, item := range plan {
				if item.Action == ActionCreate {
					subjects = append(subjects, item.Subject)
				}
			}
			want, err := configs.CanonicalLabels()
			if err != nil {
				t.Fatal(err)
			}
			if len(subjects) != 2+len(want) {
				t.Fatalf("create items = %v, want the 2 owned rulesets and %d labels", subjects, len(want))
			}
			// The plan order is labels, then rulesets, then security
			// (computePlan); the rulesets close the create list.
			tail := subjects[len(subjects)-2:]
			if tail[0] != "ruleset "+configs.MainRulesetName || tail[1] != "ruleset "+configs.ReleaseTagsRulesetName {
				t.Errorf("ruleset creation order = %v", tail)
			}
			for _, s := range subjects[:len(subjects)-2] {
				if !strings.HasPrefix(s, "label ") {
					t.Fatalf("unexpected create item %q before the rulesets", s)
				}
			}
		})
	}
}

// TestPlanMutationsConvergeTheFake applies every applicable item of a
// drifted plan in order, then re-reads and re-plans: the point of the
// plan is that executing it converges the repository, so the assertion
// is on the second plan, not the first.
func TestPlanMutationsConvergeTheFake(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	f := newFakeGitHub(t)
	env := &execEnv{client: f.client(t), owner: "o", repo: "r"}
	pol, gov, err := planDesired(t, dir)
	if err != nil {
		t.Fatal(err)
	}

	// Drift every mutable area at once: settings, topics, the default
	// branch, both owned rulesets, a label color, the security toggles,
	// and code scanning.
	f.merge["allow_merge_commit"] = true
	f.merge["delete_branch_on_merge"] = false
	f.features["has_projects"] = true
	f.features["has_wiki"] = false
	f.topics = []string{"stale"}
	f.defaultBranch = "trunk"
	f.vulnAlerts = false
	f.autoFixes = false
	f.privateVulnReporting = false
	f.secretScanning = "disabled"
	f.pushProtection = "disabled"
	f.languages = map[string]int{"Go": 10, "Python": 10}
	seedCanonicalLabels(t, f)
	f.labels["type:bug"].color = "000000"
	pol.Security.CodeScanning = true
	pol.Repository.Visibility = "private"

	first := computePlan(dir, pol, gov, actualFor(t, env.client, "o", "r"), env)
	if !hasApplicable(first) {
		t.Fatalf("drifted state produced no applicable items: %+v", first)
	}
	applied, err := applyPlan(context.Background(), first)
	if err != nil {
		t.Fatalf("applyPlan: %v", err)
	}
	if applied == 0 {
		t.Fatal("applyPlan applied nothing on a drifted repository")
	}

	second := computePlan(dir, pol, gov, actualFor(t, env.client, "o", "r"), env)
	if left := applicableItems(second); len(left) != 0 {
		t.Errorf("plan is not idempotent after apply: %+v", left)
	}
}

// TestSecretScanningPushProtectionNeverCarriesItsOwnMutation pins the
// pairing contract: the two features are one API object, so the plan
// mutates exactly once and the push protection item never carries a
// mutation of its own. Two mutating items was the defect that made a
// planned disable re-enable the feature.
func TestSecretScanningPushProtectionNeverCarriesItsOwnMutation(t *testing.T) {
	for _, visibility := range []string{"public", "private"} {
		for _, actual := range []bool{true, false} {
			for _, want := range []bool{true, false} {
				f := newFakeGitHub(t)
				f.visibility = visibility
				f.secretScanning = statusText(actual)
				f.pushProtection = statusText(actual)
				seedCanonicalLabels(t, f)
				dir := initRepo(t, bootstrapPolicy)
				pol, gov, err := planDesired(t, dir)
				if err != nil {
					t.Fatal(err)
				}
				pol.Repository.Visibility = visibility
				pol.Security.SecretScanning = boolPtr(want)
				pol.Security.SecretScanningPushProtection = boolPtr(want)

				plan := computePlan(dir, pol, gov, readFake(t, f), &execEnv{owner: "o", repo: "r"})
				item, ok := findItem(plan, "security", "secret scanning push protection")
				if !ok {
					t.Fatalf("%s actual=%v want=%v: no push protection item in %v",
						visibility, actual, want, planItems(plan))
				}
				if item.applicable() {
					t.Errorf("%s actual=%v want=%v: push protection carries its own mutation [%s] %s",
						visibility, actual, want, item.Action, item.Detail)
				}
			}
		}
	}
}

// TestPlanMutationsCarryTheFullDesiredSecretPair applies the secret
// scanning item and asserts the wire body: the single PATCH must carry
// both statuses exactly as the policy asked, in every drift shape.
func TestPlanMutationsCarryTheFullDesiredSecretPair(t *testing.T) {
	for _, c := range []struct {
		actualScan, actualPush, wantScan, wantPush bool
	}{
		{false, false, true, true},
		{true, true, false, false},
		{true, true, false, true},
		{true, false, true, true},
		{false, true, true, false},
	} {
		name := "actual_" + ss(c.actualScan) + "push" + ss(c.actualPush) + "_want_" + ss(c.wantScan) + "push" + ss(c.wantPush)
		t.Run(name, func(t *testing.T) {
			f := newFakeGitHub(t)
			f.visibility = "public"
			f.secretScanning = statusText(c.actualScan)
			f.pushProtection = statusText(c.actualPush)
			seedCanonicalLabels(t, f)
			dir := initRepo(t, bootstrapPolicy)
			pol, gov, err := planDesired(t, dir)
			if err != nil {
				t.Fatal(err)
			}
			pol.Security.SecretScanning = boolPtr(c.wantScan)
			pol.Security.SecretScanningPushProtection = boolPtr(c.wantPush)

			env := &execEnv{client: f.client(t), owner: "o", repo: "r"}
			plan := computePlan(dir, pol, gov, actualFor(t, env.client, "o", "r"), env)
			item, ok := findItem(plan, "security", "secret scanning")
			if !ok || !item.applicable() {
				t.Fatalf("no applicable secret scanning item in %v", planItems(plan))
			}
			before := len(f.securityPatches())
			if err := item.apply(context.Background()); err != nil {
				t.Fatalf("apply: %v", err)
			}
			patches := f.securityPatches()
			if len(patches) != before+1 {
				t.Fatalf("apply sent %d security_and_analysis patch(es), want exactly 1", len(patches)-before)
			}
			last := patches[len(patches)-1]
			if last[0] != statusText(c.wantScan) || last[1] != statusText(c.wantPush) {
				t.Errorf("PATCH = %v, want (%s, %s) — one field of the pair was forced", last, statusText(c.wantScan), statusText(c.wantPush))
			}

			again := computePlan(dir, pol, gov, actualFor(t, env.client, "o", "r"), env)
			item, ok = findItem(again, "security", "secret scanning")
			if !ok {
				t.Fatal("re-plan lost the secret scanning item")
			}
			if item.Action != ActionNoop {
				t.Errorf("re-plan is not converged: [%s] %s", item.Action, item.Detail)
			}
		})
	}
}

// TestOtherSecurityTogglesPlanDrift extends the simple toggles to both
// drift directions, asserting the exact word pair each transition
// reports.
func TestOtherSecurityTogglesPlanDrift(t *testing.T) {
	toggles := []struct {
		subject string
		seed    func(*fakeGitHub, bool)
		field   func(*config.Policy, bool)
	}{
		{"Dependabot alerts", func(f *fakeGitHub, on bool) { f.vulnAlerts = on },
			func(p *config.Policy, on bool) { p.Security.Dependabot = boolPtr(on) }},
		{"Dependabot security updates", func(f *fakeGitHub, on bool) { f.autoFixes = on },
			func(p *config.Policy, on bool) { p.Security.Dependabot = boolPtr(on) }},
		{"Private vulnerability reporting", func(f *fakeGitHub, on bool) { f.privateVulnReporting = on },
			func(p *config.Policy, on bool) { p.Security.PrivateVulnerabilityReporting = boolPtr(on) }},
	}
	for _, tog := range toggles {
		for _, actual := range []bool{true, false} {
			for _, want := range []bool{true, false} {
				f := newFakeGitHub(t)
				f.visibility = "public"
				tog.seed(f, actual)
				seedCanonicalLabels(t, f)
				dir := initRepo(t, bootstrapPolicy)
				pol, gov, err := planDesired(t, dir)
				if err != nil {
					t.Fatal(err)
				}
				tog.field(pol, want)

				plan := computePlan(dir, pol, gov, readFake(t, f), &execEnv{owner: "o", repo: "r"})
				item, ok := findItem(plan, "security", tog.subject)
				if !ok {
					t.Fatalf("%s: no plan item", tog.subject)
				}
				wantAction, wantDetail := ActionNoop, stateWord(actual)
				switch {
				case actual == want:
				case want:
					wantAction, wantDetail = ActionEnable, "disabled → enabled"
				default:
					wantAction, wantDetail = ActionDisable, "enabled → disabled"
				}
				if item.Action != wantAction {
					t.Errorf("%s (actual=%v want=%v): action = %s, want %s — %s",
						tog.subject, actual, want, item.Action, wantAction, item.Detail)
				}
				if item.Detail != wantDetail {
					t.Errorf("%s (actual=%v want=%v): detail = %q, want %q",
						tog.subject, actual, want, item.Detail, wantDetail)
				}
			}
		}
	}
}

// TestApplyPlanIsPartialAndOrdered pins applyPlan's contract: items
// without a mutation are skipped, applicable items run in order, and
// the first failure stops the run and reports how many changes already
// landed (spec.md §10.6 — the user must know the partial state).
func TestApplyPlanIsPartialAndOrdered(t *testing.T) {
	var log []string
	ok := func(name string) func(context.Context) error {
		return func(context.Context) error { log = append(log, name); return nil }
	}
	plan := []PlanItem{
		{Area: "a", Action: ActionNoop, Subject: "skipped"},
		{Area: "a", Action: ActionReport, Subject: "also skipped"},
		{Area: "a", Action: ActionConflict, Subject: "conflict"},
		{Area: "a", Action: ActionUnsupported, Subject: "unsupported"},
		{Area: "b", Action: ActionCreate, Subject: "first", apply: ok("first")},
		{Area: "b", Action: ActionUpdate, Subject: "second", apply: ok("second")},
		{Area: "b", Action: ActionRename, Subject: "third", apply: ok("third")},
		{Area: "b", Action: ActionEnable, Subject: "fourth", apply: ok("fourth")},
		{Area: "b", Action: ActionDisable, Subject: "fifth", apply: ok("fifth")},
	}
	applied, err := applyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("applyPlan: %v", err)
	}
	if applied != 5 {
		t.Errorf("applied = %d, want 5 (non-mutating items are skipped)", applied)
	}
	want := []string{"first", "second", "third", "fourth", "fifth"}
	if strings.Join(log, ",") != strings.Join(want, ",") {
		t.Errorf("apply order = %v, want %v", log, want)
	}

	log = nil
	boom := errors.New("boom")
	failing := []PlanItem{
		{Area: "b", Action: ActionCreate, Subject: "one", apply: ok("one")},
		{Area: "security", Action: ActionEnable, Subject: "secret scanning", apply: func(context.Context) error { return boom }},
		{Area: "b", Action: ActionCreate, Subject: "never", apply: ok("never")},
	}
	applied, err = applyPlan(context.Background(), failing)
	if err == nil {
		t.Fatal("a failing apply must be reported")
	}
	if applied != 1 {
		t.Errorf("applied = %d, want 1 (the count before the failure)", applied)
	}
	if !errors.Is(err, boom) {
		t.Errorf("the underlying error is lost: %v", err)
	}
	for _, want := range []string{"security/secret scanning", "after 1 change(s)", "re-run bootstrap to resume"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
	if len(log) != 1 || log[0] != "one" {
		t.Errorf("apply continued past the failure: %v", log)
	}

	if applied, err := applyPlan(context.Background(), nil); applied != 0 || err != nil {
		t.Errorf("applyPlan(nil) = %d, %v; want 0, nil", applied, err)
	}
}

// TestExitCode pins the three documented outcomes.
func TestExitCode(t *testing.T) {
	if got := ExitCode(Result{}, errors.New("boom")); got != 2 {
		t.Errorf("error case = %d, want 2", got)
	}
	if got := ExitCode(Result{}, nil); got != 1 {
		t.Errorf("unconverged case = %d, want 1", got)
	}
	if got := ExitCode(Result{Converged: true}, nil); got != 0 {
		t.Errorf("converged case = %d, want 0", got)
	}
}

// TestSplitSlugRejectsMalformedSlugs pins the slug parser that names
// the target repository.
func TestSplitSlugRejectsMalformedSlugs(t *testing.T) {
	for _, c := range []struct {
		slug      string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{slug: "o/r", wantOwner: "o", wantRepo: "r"},
		{slug: "o/r.git", wantOwner: "o", wantRepo: "r"},
		{slug: "https://github.com/o/r.git", wantOwner: "https:", wantRepo: "/github.com/o/r"},
		{slug: "o", wantErr: true},
		{slug: "/r", wantErr: true},
		{slug: "o/", wantErr: true},
		{slug: "", wantErr: true},
	} {
		owner, repo, err := splitSlug(c.slug)
		if c.wantErr {
			if err == nil {
				t.Errorf("splitSlug(%q) = %q, %q, nil; want an error", c.slug, owner, repo)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitSlug(%q): %v", c.slug, err)
			continue
		}
		if owner != c.wantOwner || repo != c.wantRepo {
			t.Errorf("splitSlug(%q) = %q, %q; want %q, %q", c.slug, owner, repo, c.wantOwner, c.wantRepo)
		}
	}
}

// TestLoadDesiredRefusesWithoutAValidPolicy proves bootstrap cannot
// plan without one: that is an exit-2 class error, never a silent
// empty plan.
func TestLoadDesiredRefusesWithoutAValidPolicy(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	if _, _, err := loadDesired(dir); err != nil {
		t.Fatalf("a valid fixture must load: %v", err)
	}
	writeFile(t, dir, "repoforge.yaml", "standard:\n  version: v1\nrepository:\n  governance_profile: nonesuch\n")
	if _, _, err := loadDesired(dir); err == nil {
		t.Error("a policy with violations must be refused")
	}
	writeFile(t, dir, "repoforge.yaml", "")
	if _, _, err := loadDesired(dir); err == nil {
		t.Error("an empty policy file must be refused")
	}
	bare := t.TempDir()
	if _, _, err := loadDesired(bare); err == nil {
		t.Error("a directory with no policy file must be refused")
	}
}

// TestRunDefaultsStartDirToWorkingDirectory pins the documented default
// (an empty StartDir means "here"), which the CLI relies on.
func TestRunDefaultsStartDirToWorkingDirectory(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	restore := chdir(t, dir)
	defer restore()

	_, err := Run(context.Background(), Options{Repo: "o/r", DryRun: true})
	if err == nil {
		t.Fatal("Run with no client must fail: no credential is resolvable in an isolated PATH")
	}
	if strings.Contains(err.Error(), "no policy file") {
		t.Errorf("the working directory was not used as the start directory: %v", err)
	}
}

// chdir changes the working directory for the duration of a test.
func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatal(err)
		}
	}
}

// helpers ---------------------------------------------------------------

// planDesired loads the desired state (policy plus governance document)
// of a fixture repository, which is what a plan case starts from. The
// policy is re-read from disk every time, so a case that mutates it
// cannot leak its mutation into the next case.
func planDesired(t *testing.T, dir string) (*config.Policy, *config.Governance, error) {
	t.Helper()
	_, pol, err := loadDesired(dir)
	if err != nil {
		return nil, nil, err
	}
	gov, err := doctor.LoadProfile(pol)
	if err != nil {
		return nil, nil, err
	}
	return pol, gov, nil
}

// seedConformingRulesets gives the fake the two owned rulesets exactly
// as the policy wants them, for the no-op and convergence branches.
func seedConformingRulesets(t *testing.T, f *fakeGitHub, dir string) {
	t.Helper()
	_, pol, err := loadDesired(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedRulesetFor(t, f, pol, dir)
}

// seedRulesetFor seeds the owned rulesets for a specific policy and
// repository root, so the derived desired state is identical to what
// the plan will compute.
func seedRulesetFor(t *testing.T, f *fakeGitHub, pol *config.Policy, root string) {
	t.Helper()
	wantMain, wantTags, err := configs.DesiredRulesets(pol.GovernanceProfileName(),
		config.DiscoverCheckContexts(root), pol.MergeMethods())
	if err != nil {
		t.Fatal(err)
	}
	seedRuleset(t, f, 101, wantMain)
	seedRuleset(t, f, 102, wantTags)
}

// seedCanonicalLabelsInto gives the fake the canonical label set exactly
// as the policy wants it, for the no-op and update branches.
func seedCanonicalLabelsInto(t *testing.T, f *fakeGitHub, _ string) {
	t.Helper()
	seedCanonicalLabels(t, f)
}

// seedNoLabels is the explicit "this repository has no labels at all"
// setup, so the create branch is not masked by a seeded label set.
func seedNoLabels(_ *testing.T, _ *fakeGitHub, _ string) {}

// removeWorkflows drops the fixture's CI so DiscoverCheckContexts finds
// nothing.
func removeWorkflows(t *testing.T, dir string) {
	t.Helper()
	if err := os.RemoveAll(dir + "/.github"); err != nil {
		t.Fatal(err)
	}
}

// readFake reads the remote state of the fake exactly as the apply path
// does.
func readFake(t *testing.T, f *fakeGitHub) *actualState {
	t.Helper()
	s, err := readActual(context.Background(), f.client(t), "o", "r")
	if err != nil {
		t.Fatalf("readActual: %v", err)
	}
	return s
}

// actualFor is readActual against an explicit client, used by the
// convergence test which applies mutations before re-reading.
func actualFor(t *testing.T, client *ghub.Client, owner, repo string) *actualState {
	t.Helper()
	s, err := readActual(context.Background(), client, owner, repo)
	if err != nil {
		t.Fatalf("readActual: %v", err)
	}
	return s
}

func findItem(plan []PlanItem, area, subject string) (PlanItem, bool) {
	for _, p := range plan {
		if p.Area == area && p.Subject == subject {
			return p, true
		}
	}
	return PlanItem{}, false
}

// planItems renders every item of a plan for a failure message.
func planItems(plan []PlanItem) []string {
	out := make([]string, 0, len(plan))
	for _, p := range plan {
		out = append(out, p.Area+"/"+p.Subject+"="+string(p.Action))
	}
	return out
}

func applicableItems(plan []PlanItem) []PlanItem {
	var out []PlanItem
	for _, p := range plan {
		if p.applicable() {
			out = append(out, p)
		}
	}
	return out
}
