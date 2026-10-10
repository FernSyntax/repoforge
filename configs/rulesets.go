package configs

import (
	"encoding/json"
	"fmt"

	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// Ruleset ownership convention: RepoForge-owned rulesets are
// identified by these deterministic names. The rulesets API offers no
// metadata field, so the name is the ownership metadata (verified
// against the current REST API documentation). Anything else is
// unmanaged: reported, never replaced.
const (
	MainRulesetName        = "repoforge:main"
	ReleaseTagsRulesetName = "repoforge:release-tags"
)

// RulesetPayload loads a canonical ruleset payload by file name
// ("main-solo", "main-team", "main-strict", "tags").
func RulesetPayload(name string) (*ghub.RulesetPayload, error) {
	data, err := GitHub.ReadFile("github/rulesets/" + name + ".json")
	if err != nil {
		return nil, fmt.Errorf("configs: ruleset payload %q: %w", name, err)
	}
	var p ghub.RulesetPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("configs: ruleset payload %q: %w", name, err)
	}
	return &p, nil
}

// DesiredRulesets composes the canonical payloads for a governance
// profile with repository-specific overlays:
//   - required status check contexts come from the repository's own CI
//     job names (empty list drops the rule, matching plan behavior)
//   - allowed merge methods come from the repository's contract policy
//
// profile is one of solo|team|strict.
func DesiredRulesets(profile string, contexts, mergeMethods []string) (main, tags *ghub.RulesetPayload, err error) {
	switch profile {
	case "solo", "team", "strict":
	default:
		return nil, nil, fmt.Errorf("configs: unknown governance profile %q", profile)
	}
	main, err = RulesetPayload("main-" + profile)
	if err != nil {
		return nil, nil, err
	}
	for i, r := range main.Rules {
		if r.Type == "pull_request" {
			main.Rules[i].Parameters["allowed_merge_methods"] = mergeMethods
		}
	}
	if len(contexts) == 0 {
		filtered := main.Rules[:0]
		for _, r := range main.Rules {
			if r.Type != "required_status_checks" {
				filtered = append(filtered, r)
			}
		}
		main.Rules = filtered
	} else {
		checks := make([]map[string]any, 0, len(contexts))
		for _, c := range contexts {
			checks = append(checks, map[string]any{"context": c})
		}
		for i, r := range main.Rules {
			if r.Type == "required_status_checks" {
				main.Rules[i].Parameters["required_status_checks"] = checks
			}
		}
	}
	tags, err = RulesetPayload("tags")
	if err != nil {
		return nil, nil, err
	}
	return main, tags, nil
}
