package config

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// PatternRule is a documented pattern rule (operator, pattern,
// optional negate) as used by commit metadata patterns and
// tag_name_pattern. Both are Enterprise-only capabilities (the
// Terraform provider documents tag_name_pattern as inapplicable to
// individuals and regular organizations), so they are parsed here but
// not realized in the canonical RepoForge ruleset payloads.
type PatternRule struct {
	Operator string `yaml:"operator"`
	Pattern  string `yaml:"pattern"`
	Negate   bool   `yaml:"negate"`
}

// Governance is the GitHub governance document model
// (schemas/github-governance.schema.yaml, v1). It is the desired-state
// source for doctor's remote ruleset and security comparisons.
type Governance struct {
	Version string `yaml:"version"`
	Profile string `yaml:"profile"`

	Repository struct {
		Visibility    string   `yaml:"visibility"`
		Description   string   `yaml:"description"`
		Topics        []string `yaml:"topics"`
		DefaultBranch string   `yaml:"default_branch"`
		Features      struct {
			Issues      bool `yaml:"issues"`
			Discussions bool `yaml:"discussions"`
			Projects    bool `yaml:"projects"`
			Wiki        bool `yaml:"wiki"`
		} `yaml:"features"`
	} `yaml:"repository"`

	Merge struct {
		AllowSquashMerge     bool `yaml:"allow_squash_merge"`
		AllowMergeCommit     bool `yaml:"allow_merge_commit"`
		AllowRebaseMerge     bool `yaml:"allow_rebase_merge"`
		DeleteBranchOnMerge  bool `yaml:"delete_branch_on_merge"`
		RequireLinearHistory bool `yaml:"require_linear_history"`
		AllowAutoMerge       bool `yaml:"allow_auto_merge"`
	} `yaml:"merge"`

	Rulesets struct {
		Main struct {
			Enforcement string `yaml:"enforcement"`
			Rules       struct {
				PullRequest struct {
					RequiredApprovals             int  `yaml:"required_approvals"`
					DismissStaleReviews           bool `yaml:"dismiss_stale_reviews"`
					RequireLastPushApproval       bool `yaml:"require_last_push_approval"`
					RequireConversationResolution bool `yaml:"require_conversation_resolution"`
					RequireCodeOwnerReview        bool `yaml:"require_code_owner_review"`
				} `yaml:"pull_request"`
				RequiredStatusChecks struct {
					Contexts []string `yaml:"contexts"`
					Strict   bool     `yaml:"strict"`
				} `yaml:"required_status_checks"`
				RequireLinearHistory bool `yaml:"require_linear_history"`
				RequireSignedCommits bool `yaml:"require_signed_commits"`
				BlockForcePushes     bool `yaml:"block_force_pushes"`
				BlockDeletions       bool `yaml:"block_deletions"`
				// Enterprise-plan capability (optional in the schema).
				MetadataRestrictions struct {
					CommitMessagePattern     *PatternRule `yaml:"commit_message_pattern"`
					CommitAuthorEmailPattern *PatternRule `yaml:"commit_author_email_pattern"`
				} `yaml:"metadata_restrictions"`
			} `yaml:"rules"`
		} `yaml:"main"`
		ReleaseTags struct {
			Enforcement string   `yaml:"enforcement"`
			Patterns    []string `yaml:"patterns"`
			Rules       struct {
				BlockDeletions bool `yaml:"block_deletions"`
				BlockUpdates   bool `yaml:"block_updates"`
				// Enterprise-only (like commit metadata patterns);
				// parsed here, not realized in the canonical payloads.
				TagNamePattern *PatternRule `yaml:"tag_name_pattern"`
			} `yaml:"rules"`
		} `yaml:"release_tags"`
	} `yaml:"rulesets"`

	// Security holds the tri-state settings (required|preferred|disabled).
	Security map[string]string `yaml:"security"`
}

// LoadGovernance parses a governance document. Unknown fields are
// rejected (spec.md §9.2).
func LoadGovernance(r io.Reader) (*Governance, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	g := &Governance{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(g); err != nil {
		return nil, fmt.Errorf("governance document: %w", err)
	}
	if g.Version != "v1" {
		return nil, fmt.Errorf("governance document: unknown version %q", g.Version)
	}
	if err := ValidateGovernance(g); err != nil {
		return nil, err
	}
	return g, nil
}

// ValidateGovernance enforces the profile invariants the governance
// model depends on. A profile that disables Issues or the wiki is not
// a RepoForge governance profile: the issue-driven workflow (§12) and
// the seeded wiki (§8.5) both require them.
func ValidateGovernance(g *Governance) error {
	var problems []string
	if !g.Repository.Features.Issues {
		problems = append(problems, "repository.features.issues must be true (Issues are the unit of work)")
	}
	if !g.Repository.Features.Wiki {
		problems = append(problems, "repository.features.wiki must be true (the wiki is the seeded documentation surface)")
	}
	if g.Profile != "" {
		switch g.Profile {
		case "baseline", "solo", "team", "strict":
		default:
			problems = append(problems, fmt.Sprintf("unknown profile %q (want baseline, solo, team, strict)", g.Profile))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("governance document: %s", strings.Join(problems, "; "))
	}
	return nil
}
