package doctor

import (
	"context"
	"fmt"
	"strings"

	"github.com/FernSyntax/repoforge/configs"
	"github.com/FernSyntax/repoforge/internal/config"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// LoadProfile loads the embedded governance profile selected by the
// policy's repository.governance_profile (schema default: solo).
func LoadProfile(pol *config.Policy) (*config.Governance, error) {
	profile := pol.GovernanceProfileName()
	data, err := configs.GitHub.ReadFile("github/" + profile + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("doctor: embedded governance profile %q: %w", profile, err)
	}
	return config.LoadGovernance(strings.NewReader(string(data)))
}

// runRemote performs the read-only remote audit. Every client call is
// a GET (spec.md §11.2; asserted by tests). wantMain and wantTags are the
// desired RepoForge-owned rulesets (nil when no policy file was
// found, in which case rulesets are not audited).
func runRemote(ctx context.Context, rep *Report, client *ghub.Client, owner, repoName string, pol *config.Policy, wantMain, wantTags *ghub.RulesetPayload) error {
	repo, err := client.GetRepository(ctx, owner, repoName)
	if err != nil {
		// A failed read is an incomplete audit, not drift. The finding
		// stays visible to a human, but the error is returned so the
		// caller exits 2 (spec.md §11.4) instead of exiting 1 and blocking
		// merges on what looks like repository drift (spec.md §7.4).
		rep.add(Fail, "remote/repository", "cannot read repository: %v", err)
		return err
	}
	rep.add(Pass, "remote/repository", "repository %s/%s is accessible", owner, repoName)

	if pol != nil {
		checkRepositorySettings(rep, repo, pol)
		checkMergeSettings(rep, repo, pol)
		checkFeatureSettings(rep, repo, pol)
	}

	topics, err := client.ListTopics(ctx, owner, repoName)
	if err != nil {
		rep.add(Warn, "remote/topics", "cannot read topics: %v", err)
	} else if len(topics.Names) == 0 {
		rep.add(Pass, "remote/topics", "no topics set (topics are not policy-managed)")
	} else {
		rep.add(Pass, "remote/topics", "repository topics: %s", strings.Join(topics.Names, ", "))
	}

	if wantMain != nil {
		checkRulesets(ctx, rep, client, owner, repoName, wantMain, wantTags)
	}
	checkLabels(ctx, rep, client, owner, repoName)
	if pol != nil {
		checkSecurity(ctx, rep, client, owner, repoName, repo, pol)
	}
	return nil
}

func checkRepositorySettings(rep *Report, repo *ghub.Repository, pol *config.Policy) {
	wantVis := pol.Repository.Visibility
	if wantVis == "" {
		wantVis = "public"
	}
	if repo.Visibility == wantVis {
		rep.add(Pass, "remote/visibility", "visibility is %s", repo.Visibility)
	} else {
		rep.add(Fail, "remote/visibility", "visibility is %q, policy wants %q", repo.Visibility, wantVis)
	}
	wantBranch := pol.DefaultBranch()
	if repo.DefaultBranch == wantBranch {
		rep.add(Pass, "remote/default-branch", "default branch is %s", repo.DefaultBranch)
	} else {
		rep.add(Fail, "remote/default-branch", "default branch is %q, policy wants %q", repo.DefaultBranch, wantBranch)
	}
}

func boolFinding(rep *Report, check, name string, actual, want bool) {
	if actual == want {
		rep.add(Pass, check, "%s %s", name, onOff(want))
	} else {
		rep.add(Fail, check, "%s %s, policy wants %s", name, onOff(actual), onOff(want))
	}
}

func onOff(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

func checkMergeSettings(rep *Report, repo *ghub.Repository, pol *config.Policy) {
	boolFinding(rep, "remote/merge", "squash merge", repo.AllowSquashMerge, pol.MergeSquash())
	boolFinding(rep, "remote/merge", "merge commit", repo.AllowMergeCommit, pol.Merge.MergeCommit)
	boolFinding(rep, "remote/merge", "rebase merge", repo.AllowRebaseMerge, pol.Merge.Rebase)
	boolFinding(rep, "remote/merge", "delete branch on merge", repo.DeleteBranchOnMerge, pol.MergeDeleteBranch())
	boolFinding(rep, "remote/merge", "auto-merge", repo.AllowAutoMerge, pol.Merge.AutoMerge)
}

func checkFeatureSettings(rep *Report, repo *ghub.Repository, pol *config.Policy) {
	boolFinding(rep, "remote/features", "issues", repo.HasIssues, pol.GitHubIssues())
	boolFinding(rep, "remote/features", "discussions", repo.HasDiscussions, pol.GitHub.Discussions)
	boolFinding(rep, "remote/features", "projects", repo.HasProjects, pol.GitHub.Projects)
	boolFinding(rep, "remote/features", "wiki", repo.HasWiki, pol.GitHub.Wiki)
}

// checkRulesets audits the RepoForge-owned rulesets against the
// desired payloads (spec.md §11.1, 11.3). Ownership is the deterministic
// name (the rulesets API has no metadata field); everything else is
// unmanaged and reported, never compared.
func checkRulesets(ctx context.Context, rep *Report, client *ghub.Client, owner, repoName string, wantMain, wantTags *ghub.RulesetPayload) {
	rulesets, err := client.ListRulesets(ctx, owner, repoName)
	if err != nil {
		rep.add(Fail, "remote/rulesets", "cannot list rulesets: %v", err)
		return
	}

	checkOwnedRuleset(ctx, rep, client, owner, repoName, rulesets, wantMain)
	checkOwnedRuleset(ctx, rep, client, owner, repoName, rulesets, wantTags)

	// Unmanaged rulesets are never touched (spec.md §5.3). Bootstrap
	// reports them as conflicts, so doctor surfaces them as warnings.
	for _, rs := range rulesets {
		if rs.Name == configs.MainRulesetName || rs.Name == configs.ReleaseTagsRulesetName {
			continue
		}
		if rs.SourceType != "Repository" {
			continue
		}
		rep.add(Warn, "remote/rulesets",
			"unmanaged %s ruleset %q (enforcement %s) exists; bootstrap leaves it untouched and reports a conflict",
			rs.Target, rs.Name, rs.Enforcement)
	}
}

func checkOwnedRuleset(ctx context.Context, rep *Report, client *ghub.Client, owner, repoName string, rulesets []ghub.Ruleset, want *ghub.RulesetPayload) {
	if want == nil {
		return
	}
	var found *ghub.Ruleset
	for i := range rulesets {
		if rulesets[i].Name == want.Name {
			found = &rulesets[i]
			break
		}
	}
	if found == nil {
		rep.add(Fail, "remote/rulesets", "ruleset %q is missing; run `repoforge bootstrap` to create it", want.Name)
		return
	}
	if found.Target != want.Target {
		rep.add(Fail, "remote/rulesets",
			"ruleset %q has target %q, policy wants %q; bootstrap reports this as a conflict — review manually",
			found.Name, found.Target, want.Target)
		return
	}
	detail, err := client.GetRuleset(ctx, owner, repoName, found.ID)
	if err != nil {
		rep.add(Fail, "remote/rulesets", "cannot read ruleset %q: %v", found.Name, err)
		return
	}
	reasons := ghub.RulesetDrift(detail, want)
	if len(reasons) == 0 {
		rep.add(Pass, "remote/rulesets", "ruleset %q exists and matches the policy (enforcement %s, %d rules)",
			detail.Name, detail.Enforcement, len(want.Rules))
		return
	}
	for _, reason := range reasons {
		rep.add(Fail, "remote/rulesets", "ruleset %q: %s; run `repoforge bootstrap` to reconcile",
			detail.Name, reason)
	}
}

// checkLabels audits the canonical label set
// (configs/github/labels.yaml, spec.md §11.1, 11.3): one PASS when every
// canonical label exists and matches, one FAIL per missing or
// drifted label with the bootstrap remediation. Unrelated labels are
// user-owned and ignored.
func checkLabels(ctx context.Context, rep *Report, client *ghub.Client, owner, repoName string) {
	want, err := configs.CanonicalLabels()
	if err != nil {
		rep.add(Fail, "remote/labels", "cannot load canonical labels: %v", err)
		return
	}
	actual, err := client.ListLabels(ctx, owner, repoName)
	if err != nil {
		rep.add(Fail, "remote/labels", "cannot list labels: %v", err)
		return
	}
	byName := make(map[string]ghub.Label, len(actual))
	for _, l := range actual {
		byName[l.Name] = l
	}
	var matched int
	for _, w := range want {
		got, ok := byName[w.Name]
		if !ok {
			rep.add(Fail, "remote/labels", "label %q is missing; run `repoforge bootstrap` to create it", w.Name)
			continue
		}
		if ghub.NormalizeLabelColor(got.Color) != ghub.NormalizeLabelColor(w.Color) ||
			strings.TrimSpace(got.Description) != w.Description {
			rep.add(Fail, "remote/labels",
				"label %q is color %q / %q, policy wants color %q / %q; run `repoforge bootstrap` to reconcile",
				w.Name, got.Color, got.Description, w.Color, w.Description)
			continue
		}
		matched++
	}
	if matched == len(want) {
		rep.add(Pass, "remote/labels", "%d canonical labels exist and match the policy", len(want))
	}
}

func checkSecurity(ctx context.Context, rep *Report, client *ghub.Client, owner, repoName string, repo *ghub.Repository, pol *config.Policy) {
	s, err := client.GetSecuritySettings(ctx, owner, repoName)
	if err != nil {
		rep.add(Fail, "remote/security", "cannot inspect security settings: %v", err)
		return
	}
	public := repo.Visibility == "public"

	// Dependabot: available on all plans, so disabled-when-required is
	// drift, never a plan gap.
	boolFinding(rep, "remote/security", "Dependabot alerts", s.DependabotAlertsEnabled, pol.SecurityDependabot())
	boolFinding(rep, "remote/security", "Dependabot security updates", s.AutomatedSecurityFixesEnabled, pol.SecurityDependabot())

	// Private vulnerability reporting: probed directly (GET returns
	// {"enabled"}), so a mismatch is drift. Documented for public
	// repositories; elsewhere the API is the source of truth and
	// apply-time failures surface explicitly.
	boolFinding(rep, "remote/security", "Private vulnerability reporting", s.PrivateVulnerabilityReporting, pol.SecurityPrivateVulnerabilityReporting())

	checkPlanSecurity(rep, "remote/security", "secret scanning", s.SecretScanning, pol.SecuritySecretScanning(), public, "GitHub Secret Protection")
	checkPlanSecurity(rep, "remote/security", "secret scanning push protection", s.SecretScanningPushProtection, pol.SecuritySecretScanningPushProtection(), public, "GitHub Secret Protection")

	// Code scanning: presence is probed via the default-setup endpoint.
	codeScanningOn := s.CodeScanningDefaultSetup == "configured"
	wantCodeScanning := pol.Security.CodeScanning
	switch {
	case codeScanningOn && wantCodeScanning:
		rep.add(Pass, "remote/security", "code scanning configured")
	case codeScanningOn && !wantCodeScanning:
		rep.add(Warn, "remote/security", "code scanning configured but policy does not require it")
	case !codeScanningOn && !wantCodeScanning:
		rep.add(Pass, "remote/security", "code scanning not configured (policy does not require it)")
	case public:
		rep.add(Fail, "remote/security", "code scanning not configured (free for public repositories)")
	default:
		rep.add(Unsupported, "remote/security", "code scanning unavailable on this plan (private repositories require GitHub Code Security)")
	}

	// Dependency review is enforced in CI (dependency-review-action);
	// no repository API verifies it.
	if pol.SecurityDependencyReview() {
		rep.add(Unsupported, "remote/security", "dependency review is CI-enforced (dependency-review-action); not verifiable via the repository API")
	} else {
		rep.add(Pass, "remote/security", "dependency review not required by policy")
	}
}

// checkPlanSecurity reports a security feature whose availability
// depends on plan and visibility. Disabled-on-public-when-required is
// drift (FAIL); disabled-on-private is a capability gap (UNSUPPORTED),
// never a failure.
func checkPlanSecurity(rep *Report, check, name, status string, want, public bool, product string) {
	switch {
	case status == "enabled" && want:
		rep.add(Pass, check, "%s enabled", name)
	case status == "enabled" && !want:
		rep.add(Warn, check, "%s enabled but policy disables it", name)
	case status != "enabled" && !want:
		rep.add(Pass, check, "%s disabled per policy", name)
	case public:
		rep.add(Fail, check, "%s disabled (free for public repositories)", name)
	default:
		rep.add(Unsupported, check, "%s disabled; private repositories require %s", name, product)
	}
}
