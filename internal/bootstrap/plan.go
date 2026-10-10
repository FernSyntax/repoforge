package bootstrap

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/FernSyntax/repoforge/configs"
	"github.com/FernSyntax/repoforge/internal/config"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// RepoForge-owned ruleset names (docs/architecture/github-governance.md).
const (
	mainRulesetName        = configs.MainRulesetName
	releaseTagsRulesetName = configs.ReleaseTagsRulesetName
)

// codeQLLanguages maps GitHub language names to CodeQL language keys.
var codeQLLanguages = map[string]string{
	"c": "c", "c++": "cpp", "c#": "csharp", "go": "go", "java": "java",
	"javascript": "javascript", "kotlin": "kotlin", "python": "python",
	"ruby": "ruby", "rust": "rust", "swift": "swift", "typescript": "typescript",
	"actions": "actions",
}

// computePlan reconciles desired and actual state into ordered plan
// items. It performs no mutations.
func computePlan(root string, pol *config.Policy, gov *config.Governance, actual *actualState, env *execEnv) []PlanItem {
	var plan []PlanItem
	add := func(area string, action Action, subject, detail string, apply func(context.Context) error) {
		plan = append(plan, PlanItem{Area: area, Action: action, Subject: subject, Detail: detail, apply: apply})
	}

	// Repository metadata: visibility is report-only (spec.md §8.5 —
	// security-sensitive; bootstrap never changes it).
	wantVis := pol.Repository.Visibility
	if wantVis == "" {
		wantVis = "public"
	}
	if actual.repo.Visibility != wantVis {
		add("repository", ActionReport, "visibility",
			fmt.Sprintf("is %q, policy wants %q; visibility changes are never automated (spec.md §8.5) — change manually", actual.repo.Visibility, wantVis), nil)
	} else {
		add("repository", ActionNoop, "visibility", fmt.Sprintf("is %s", wantVis), nil)
	}

	// Merge + feature settings: one PATCH for all drifted fields.
	patch, changed := mergePatch(pol, actual.repo)
	if len(changed) > 0 {
		p := patch
		add("repository", ActionUpdate, "settings",
			"update: "+strings.Join(changed, ", "),
			func(ctx context.Context) error { return env.client.UpdateRepository(ctx, env.owner, env.repo, p) })
	} else {
		add("repository", ActionNoop, "settings", "merge and feature settings match policy", nil)
	}

	// Topics: managed only when the governance profile declares a
	// non-empty set; otherwise user-owned, left untouched.
	govTopics := gov.Repository.Topics
	if len(govTopics) > 0 {
		if !sameStringSet(actual.topics, govTopics) {
			names := append([]string(nil), govTopics...)
			sort.Strings(names)
			add("repository", ActionUpdate, "topics",
				fmt.Sprintf("replace %v with %v", actual.topics, names),
				func(ctx context.Context) error { return env.client.ReplaceTopics(ctx, env.owner, env.repo, names) })
		} else {
			add("repository", ActionNoop, "topics", "topics match policy", nil)
		}
	} else {
		add("repository", ActionReport, "topics",
			fmt.Sprintf("not policy-managed; %d topic(s) present, left untouched", len(actual.topics)), nil)
	}

	// Default branch rename (documented rename endpoint).
	wantBranch := pol.DefaultBranch()
	if actual.repo.DefaultBranch != wantBranch {
		from := actual.repo.DefaultBranch
		add("repository", ActionRename, "default branch",
			fmt.Sprintf("rename %q to %q (open PRs are retargeted by GitHub)", from, wantBranch),
			func(ctx context.Context) error {
				return env.client.RenameBranch(ctx, env.owner, env.repo, from, wantBranch)
			})
	} else {
		add("repository", ActionNoop, "default branch", fmt.Sprintf("is %s", wantBranch), nil)
	}

	// Labels: RepoForge-owned by exact canonical name, reconciled
	// below; everything else is user-owned.
	plan = append(plan, planLabels(actual, env)...)
	plan = append(plan, planRulesets(root, pol, gov, actual, env)...)
	plan = append(plan, planSecurity(pol, actual, env)...)
	return plan
}

// planLabels reconciles the canonical label set
// (configs/github/labels.yaml): missing labels are created, drifted
// color/description updated. Any other label is user-owned: preserved,
// never modified or deleted (the client has no label-delete path).
func planLabels(actual *actualState, env *execEnv) []PlanItem {
	var plan []PlanItem
	add := func(action Action, subject, detail string, apply func(context.Context) error) {
		plan = append(plan, PlanItem{Area: "labels", Action: action, Subject: subject, Detail: detail, apply: apply})
	}
	want, err := configs.CanonicalLabels()
	if err != nil {
		add(ActionReport, "labels", fmt.Sprintf("cannot load canonical labels: %v", err), nil)
		return plan
	}
	owned := make(map[string]configs.CanonicalLabel, len(want))
	for _, w := range want {
		owned[w.Name] = w
	}
	actualByName := make(map[string]ghub.Label, len(actual.labels))
	var unrelated int
	for _, l := range actual.labels {
		actualByName[l.Name] = l
		if _, ok := owned[l.Name]; !ok {
			unrelated++
		}
	}
	var matched int
	for _, w := range want {
		got, ok := actualByName[w.Name]
		if !ok {
			w := w
			add(ActionCreate, "label "+w.Name,
				fmt.Sprintf("create label (color %s)", w.Color),
				func(ctx context.Context) error {
					return env.client.CreateLabel(ctx, env.owner, env.repo,
						ghub.LabelPayload{Name: w.Name, Color: w.Color, Description: w.Description})
				})
			continue
		}
		if ghub.NormalizeLabelColor(got.Color) != ghub.NormalizeLabelColor(w.Color) ||
			strings.TrimSpace(got.Description) != w.Description {
			w := w
			got := got
			add(ActionUpdate, "label "+w.Name,
				fmt.Sprintf("color %q → %q, description %q → %q", got.Color, w.Color, got.Description, w.Description),
				func(ctx context.Context) error {
					return env.client.UpdateLabel(ctx, env.owner, env.repo, w.Name,
						ghub.LabelPayload{Color: w.Color, Description: w.Description})
				})
			continue
		}
		matched++
	}
	if matched == len(want) {
		add(ActionNoop, "labels", fmt.Sprintf("%d canonical labels match policy", len(want)), nil)
	}
	if unrelated > 0 {
		add(ActionReport, "labels", fmt.Sprintf("%d unrelated label(s) present, left untouched", unrelated), nil)
	}
	return plan
}

func planRulesets(root string, pol *config.Policy, gov *config.Governance, actual *actualState, env *execEnv) []PlanItem {
	var plan []PlanItem
	add := func(action Action, subject, detail string, apply func(context.Context) error) {
		plan = append(plan, PlanItem{Area: "rulesets", Action: action, Subject: subject, Detail: detail, apply: apply})
	}

	// Report unmanaged rulesets; never overwrite or delete them.
	for _, rs := range actual.rulesets {
		owned := (rs.Target == "branch" && rs.Name == mainRulesetName) ||
			(rs.Target == "tag" && rs.Name == releaseTagsRulesetName)
		if owned {
			continue
		}
		if rs.SourceType != "Repository" {
			add(ActionReport, "ruleset "+rs.Name, fmt.Sprintf("inherited from %s (target %s); read-only at repository level", rs.SourceType, rs.Target), nil)
			continue
		}
		add(ActionConflict, "ruleset "+rs.Name,
			fmt.Sprintf("unmanaged %s ruleset (enforcement %s) exists; left untouched — review manually", rs.Target, rs.Enforcement), nil)
	}

	contexts := config.DiscoverCheckContexts(root)
	if len(contexts) == 0 {
		add(ActionReport, "status checks", "no CI job contexts discovered; required_status_checks rule omitted", nil)
	}
	profile := gov.Profile
	if profile == "" {
		profile = pol.GovernanceProfileName()
	}
	wantMain, wantTags, err := configs.DesiredRulesets(profile, contexts, pol.MergeMethods())
	if err != nil {
		add(ActionReport, "rulesets", fmt.Sprintf("cannot compose desired rulesets: %v", err), nil)
		return plan
	}
	if actual.mainDetail == nil {
		p := wantMain
		add(ActionCreate, "ruleset "+mainRulesetName,
			fmt.Sprintf("create branch ruleset (enforcement %s, %d rules)", p.Enforcement, len(p.Rules)),
			func(ctx context.Context) error {
				_, err := env.client.CreateRuleset(ctx, env.owner, env.repo, *p)
				return err
			})
	} else if ghub.RulesetDrifted(actual.mainDetail, wantMain) {
		p := wantMain
		id := actual.mainDetail.ID
		add(ActionUpdate, "ruleset "+mainRulesetName, "update branch ruleset to match governance profile",
			func(ctx context.Context) error { return env.client.UpdateRuleset(ctx, env.owner, env.repo, id, *p) })
	} else {
		add(ActionNoop, "ruleset "+mainRulesetName, "matches governance profile", nil)
	}

	if actual.tagsDetail == nil {
		p := wantTags
		add(ActionCreate, "ruleset "+releaseTagsRulesetName,
			fmt.Sprintf("create tag ruleset over %v", p.Conditions.RefName.Include),
			func(ctx context.Context) error {
				_, err := env.client.CreateRuleset(ctx, env.owner, env.repo, *p)
				return err
			})
	} else if ghub.RulesetDrifted(actual.tagsDetail, wantTags) {
		p := wantTags
		id := actual.tagsDetail.ID
		add(ActionUpdate, "ruleset "+releaseTagsRulesetName, "update tag ruleset to match governance profile",
			func(ctx context.Context) error { return env.client.UpdateRuleset(ctx, env.owner, env.repo, id, *p) })
	} else {
		add(ActionNoop, "ruleset "+releaseTagsRulesetName, "matches governance profile", nil)
	}
	return plan
}

func planSecurity(pol *config.Policy, actual *actualState, env *execEnv) []PlanItem {
	var plan []PlanItem
	add := func(action Action, subject, detail string, apply func(context.Context) error) {
		plan = append(plan, PlanItem{Area: "security", Action: action, Subject: subject, Detail: detail, apply: apply})
	}
	s := actual.security
	public := actual.repo.Visibility == "public"

	toggle := func(subject string, actualOn, want bool, apply func(context.Context) error) {
		switch {
		case actualOn == want:
			add(ActionNoop, subject, stateWord(actualOn), nil)
		case want:
			add(ActionEnable, subject, "disabled → enabled", apply)
		default:
			add(ActionDisable, subject, "enabled → disabled", apply)
		}
	}

	dependabot := pol.SecurityDependabot()
	toggle("Dependabot alerts", s.DependabotAlertsEnabled, dependabot,
		func(ctx context.Context) error {
			return env.client.SetVulnerabilityAlerts(ctx, env.owner, env.repo, dependabot)
		})
	toggle("Dependabot security updates", s.AutomatedSecurityFixesEnabled, dependabot,
		func(ctx context.Context) error {
			return env.client.SetAutomatedSecurityFixes(ctx, env.owner, env.repo, dependabot)
		})
	privateVulnReporting := pol.SecurityPrivateVulnerabilityReporting()
	toggle("Private vulnerability reporting", s.PrivateVulnerabilityReporting, privateVulnReporting,
		func(ctx context.Context) error {
			return env.client.SetPrivateVulnerabilityReporting(ctx, env.owner, env.repo, privateVulnReporting)
		})

	// Secret scanning and push protection are one API object: a PATCH of
	// security_and_analysis sends both fields together. They are therefore
	// planned and applied as a single item carrying the complete desired
	// pair. Splitting them into two items with independent callbacks made
	// each callback force its own feature to true, so a plan to disable
	// either one re-enabled it — and the post-apply re-read then reported
	// drift forever.
	secretScanning := pol.SecuritySecretScanning()
	pushProtection := pol.SecuritySecretScanningPushProtection()
	actualScanning := s.SecretScanning == "enabled"
	actualPush := s.SecretScanningPushProtection == "enabled"

	// Enabling either feature on a private repository needs a GitHub
	// Advanced Security product; that transition can never converge, so
	// it is classified up front as an explicit unsupported item instead
	// of attempting a doomed mutation. Disabling is always permitted.
	needsProduct := (secretScanning && !actualScanning) || (pushProtection && !actualPush)

	switch {
	case actualScanning == secretScanning && actualPush == pushProtection:
		add(ActionNoop, "secret scanning", stateWord(actualScanning), nil)
		add(ActionNoop, "secret scanning push protection", stateWord(actualPush), nil)

	case needsProduct && !public:
		reason := "disabled; enabling requires a GitHub Advanced Security product on this private repository — set security policy explicitly or upgrade"
		if !secretScanning && !actualScanning {
			// Scanning stays disabled on policy: only push protection
			// is blocked, so report it separately and do not disable
			// scanning on the way through.
			add(ActionNoop, "secret scanning", "disabled per policy", nil)
		} else {
			add(ActionUnsupported, "secret scanning", reason, nil)
		}
		add(ActionUnsupported, "secret scanning push protection", reason, nil)

	default:
		detail := fmt.Sprintf(
			"secret scanning %s → %s, push protection %s → %s (applied together: they share one API object)",
			stateWord(actualScanning), stateWord(secretScanning),
			stateWord(actualPush), stateWord(pushProtection))
		switch {
		case secretScanning && !actualScanning, pushProtection && !actualPush:
			add(ActionEnable, "secret scanning", detail, func(ctx context.Context) error {
				return env.client.SetSecretScanning(ctx, env.owner, env.repo, secretScanning, pushProtection)
			})
			add(ActionNoop, "secret scanning push protection", stateWord(actualPush), nil)
		case !secretScanning && actualScanning, !pushProtection && actualPush:
			add(ActionDisable, "secret scanning", detail, func(ctx context.Context) error {
				return env.client.SetSecretScanning(ctx, env.owner, env.repo, secretScanning, pushProtection)
			})
			add(ActionNoop, "secret scanning push protection", stateWord(actualPush), nil)
		default:
			// A single toggle moving while the other already matches:
			// still one API object, so still one mutation carrying both.
			add(ActionUpdate, "secret scanning", detail, func(ctx context.Context) error {
				return env.client.SetSecretScanning(ctx, env.owner, env.repo, secretScanning, pushProtection)
			})
			add(ActionNoop, "secret scanning push protection", stateWord(actualPush), nil)
		}
	}

	codeScanningOn := s.CodeScanningDefaultSetup == "configured"
	wantCodeScanning := pol.Security.CodeScanning
	switch {
	case codeScanningOn == wantCodeScanning:
		add(ActionNoop, "code scanning", stateWord(codeScanningOn), nil)
	case wantCodeScanning && !public:
		add(ActionUnsupported, "code scanning",
			"not configured; private repositories require GitHub Code Security", nil)
	case wantCodeScanning:
		langs := codeQLCandidates(actual.languages)
		if len(langs) == 0 {
			add(ActionUnsupported, "code scanning", "no CodeQL-supported languages detected", nil)
		} else {
			add(ActionEnable, "code scanning",
				fmt.Sprintf("configure CodeQL default setup (%s)", strings.Join(langs, ", ")),
				func(ctx context.Context) error {
					return env.client.ConfigureCodeScanningDefaultSetup(ctx, env.owner, env.repo, langs)
				})
		}
	default:
		add(ActionReport, "code scanning", "configured but policy does not require it; left enabled (never silently disable security features)", nil)
	}

	if pol.SecurityDependencyReview() {
		add(ActionReport, "dependency review", "enforced in CI via dependency-review-action; no repository API to manage it", nil)
	} else {
		add(ActionNoop, "dependency review", "not required by policy", nil)
	}
	return plan
}

func stateWord(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

func codeQLCandidates(languages map[string]int) []string {
	var out []string
	for name := range languages {
		if key, ok := codeQLLanguages[strings.ToLower(name)]; ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
