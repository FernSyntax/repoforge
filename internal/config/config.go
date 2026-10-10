// Package config loads and validates the declarative RepoForge
// repository policy (spec.md §9).
//
// The policy file is repoforge.yaml (or .yml/.json) at the managed
// repository root. Loading is strict: unknown fields are rejected
// (spec.md §9.2) and every field is validated semantically. Defaults follow
// the schema annotations (spec.md §9.4).
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the repository contract schema version this package
// implements. Policies declaring any other version are rejected.
const SchemaVersion = "v1"

// PolicyFileNames are the accepted policy file locations, in probe
// order: root file first, then the .repoforge/ directory form used
// by generated projects.
var PolicyFileNames = []string{
	"repoforge.yaml",
	"repoforge.yml",
	"repoforge.json",
	".repoforge/config.yml",
	".repoforge/config.yaml",
	".repoforge/config.json",
}

// Valid enum value sets. These mirror the contract schema
// (schemas/repoforge.schema.yaml) and are drift-tested against it.
var (
	ValidGovernanceProfiles = []string{"solo", "team", "strict"}
	ValidVisibilities       = []string{"public", "private", "internal"}
	ValidCommitStyles       = []string{"conventional"}
	ValidBranchStyles       = []string{"type-issue-description"}
	ValidActionPinning      = []string{"sha", "tag", "none"}
	ValidReleaseStrategies  = []string{"none", "tag", "github-release"}
	ValidProjectKinds       = []string{"application", "cli", "library", "service", "template", "docs", "meta"}
)

// Policy is the declarative repository contract (schema v1). Fields
// that default to true in the schema are pointers so an omitted field
// is distinguishable from an explicit false (spec.md §9.4).
type Policy struct {
	Standard struct {
		Name    string `yaml:"name" json:"name"`
		Version string `yaml:"version" json:"version"`
	} `yaml:"standard" json:"standard"`
	Repository struct {
		GovernanceProfile string `yaml:"governance_profile" json:"governance_profile"`
		Visibility        string `yaml:"visibility" json:"visibility"`
		DefaultBranch     string `yaml:"default_branch" json:"default_branch"`
	} `yaml:"repository" json:"repository"`
	Merge struct {
		Squash        *bool `yaml:"squash" json:"squash"`
		MergeCommit   bool  `yaml:"merge_commit" json:"merge_commit"`
		Rebase        bool  `yaml:"rebase" json:"rebase"`
		DeleteBranch  *bool `yaml:"delete_branch" json:"delete_branch"`
		LinearHistory bool  `yaml:"linear_history" json:"linear_history"`
		AutoMerge     bool  `yaml:"auto_merge" json:"auto_merge"`
	} `yaml:"merge" json:"merge"`
	Git struct {
		CommitStyle string `yaml:"commit_style" json:"commit_style"`
		BranchStyle string `yaml:"branch_style" json:"branch_style"`
	} `yaml:"git" json:"git"`
	GitHub struct {
		Issues      *bool `yaml:"issues" json:"issues"`
		Discussions bool  `yaml:"discussions" json:"discussions"`
		Projects    bool  `yaml:"projects" json:"projects"`
		Wiki        bool  `yaml:"wiki" json:"wiki"`
	} `yaml:"github" json:"github"`
	Security struct {
		Dependabot                    *bool  `yaml:"dependabot" json:"dependabot"`
		CodeScanning                  bool   `yaml:"code_scanning" json:"code_scanning"`
		SecretScanning                *bool  `yaml:"secret_scanning" json:"secret_scanning"`
		SecretScanningPushProtection  *bool  `yaml:"secret_scanning_push_protection" json:"secret_scanning_push_protection"`
		DependencyReview              *bool  `yaml:"dependency_review" json:"dependency_review"`
		PrivateVulnerabilityReporting *bool  `yaml:"private_vulnerability_reporting" json:"private_vulnerability_reporting"`
		Scorecard                     *bool  `yaml:"scorecard" json:"scorecard"`
		ActionPinning                 string `yaml:"action_pinning" json:"action_pinning"`
	} `yaml:"security" json:"security"`
	Release struct {
		Strategy string `yaml:"strategy" json:"strategy"`
	} `yaml:"release" json:"release"`
	Project struct {
		Kind string `yaml:"kind" json:"kind"`
	} `yaml:"project" json:"project"`
	Ownership struct {
		ManagedPaths     []string `yaml:"managed_paths" json:"managed_paths"`
		ScaffoldedPaths  []string `yaml:"scaffolded_paths" json:"scaffolded_paths"`
		UserOwnedPaths   []string `yaml:"user_owned_paths" json:"user_owned_paths"`
		PlaceholderPaths []string `yaml:"placeholder_paths" json:"placeholder_paths"`
	} `yaml:"ownership" json:"ownership"`
}

// Effective values after schema defaults (spec.md §9.4).
func (p *Policy) MergeSquash() bool { return p.Merge.Squash == nil || *p.Merge.Squash }
func (p *Policy) MergeDeleteBranch() bool {
	return p.Merge.DeleteBranch == nil || *p.Merge.DeleteBranch
}
func (p *Policy) GitHubIssues() bool { return p.GitHub.Issues == nil || *p.GitHub.Issues }
func (p *Policy) SecurityDependabot() bool {
	return p.Security.Dependabot == nil || *p.Security.Dependabot
}
func (p *Policy) SecuritySecretScanning() bool {
	return p.Security.SecretScanning == nil || *p.Security.SecretScanning
}
func (p *Policy) SecuritySecretScanningPushProtection() bool {
	return p.Security.SecretScanningPushProtection == nil || *p.Security.SecretScanningPushProtection
}
func (p *Policy) SecurityDependencyReview() bool {
	return p.Security.DependencyReview == nil || *p.Security.DependencyReview
}
func (p *Policy) SecurityPrivateVulnerabilityReporting() bool {
	return p.Security.PrivateVulnerabilityReporting == nil || *p.Security.PrivateVulnerabilityReporting
}
func (p *Policy) SecurityScorecard() bool {
	return p.Security.Scorecard == nil || *p.Security.Scorecard
}

// MergeMethods derives the allowed_merge_methods list from the
// contract merge settings; the repository settings and the ruleset
// pull_request rule must agree (docs/architecture/github-governance.md).
func (p *Policy) MergeMethods() []string {
	var out []string
	if p.Merge.MergeCommit {
		out = append(out, "merge")
	}
	if p.MergeSquash() {
		out = append(out, "squash")
	}
	if p.Merge.Rebase {
		out = append(out, "rebase")
	}
	return out
}

func (p *Policy) ActionPinningMode() string {
	if p.Security.ActionPinning == "" {
		return "sha"
	}
	return p.Security.ActionPinning
}

func (p *Policy) DefaultBranch() string {
	if p.Repository.DefaultBranch == "" {
		return "main"
	}
	return p.Repository.DefaultBranch
}

// GovernanceProfileName returns the governance profile selected by the
// contract (repository.governance_profile), defaulting to "solo" when
// unset (schema default). It is the single place the default lives so
// bootstrap and doctor always derive the same desired rulesets.
func (p *Policy) GovernanceProfileName() string {
	if p == nil || p.Repository.GovernanceProfile == "" {
		return "solo"
	}
	return p.Repository.GovernanceProfile
}

// Violation is a single semantic policy problem.
type Violation struct {
	Field   string
	Message string
}

func (v Violation) String() string {
	if v.Field == "" {
		return v.Message
	}
	return v.Field + ": " + v.Message
}

// SyntaxError reports a policy file that cannot be parsed at all.
// Callers treat it as a configuration error (exit code 2).
type SyntaxError struct {
	Path string
	Err  error
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e *SyntaxError) Unwrap() error { return e.Err }

// FindPolicyFile returns the policy file path under root, or "" if the
// repository carries no policy.
func FindPolicyFile(root string) string {
	for _, name := range PolicyFileNames {
		p := filepath.Join(root, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// LoadPolicy reads, decodes, and semantically validates a policy file.
// A malformed file yields a *SyntaxError. Unknown fields and type
// mismatches yield violations (unknown-field detection, spec.md §9.2).
func LoadPolicy(path string) (*Policy, []Violation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, &SyntaxError{Path: path, Err: err}
	}
	p := &Policy{}
	if filepath.Ext(path) == ".json" {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(p); err != nil {
			var syn *json.SyntaxError
			if errors.As(err, &syn) {
				return nil, nil, &SyntaxError{Path: path, Err: err}
			}
			return nil, []Violation{{Field: "", Message: err.Error()}}, nil
		}
	} else {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(p); err != nil {
			var te *yaml.TypeError
			if errors.As(err, &te) {
				violations := make([]Violation, 0, len(te.Errors))
				for _, msg := range te.Errors {
					violations = append(violations, Violation{Field: "", Message: msg})
				}
				return nil, violations, nil
			}
			return nil, nil, &SyntaxError{Path: path, Err: err}
		}
	}
	return p, ValidatePolicy(p), nil
}

var (
	namePattern          = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	defaultBranchPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*$`)
)

// ValidatePolicy performs the semantic checks the schema expresses
// (plus cross-field consistency the schema cannot).
func ValidatePolicy(p *Policy) []Violation {
	var v []Violation
	add := func(field, msg string) {
		v = append(v, Violation{Field: field, Message: msg})
	}

	if p.Standard.Version == "" {
		add("standard.version", "required field missing")
	} else if p.Standard.Version != SchemaVersion {
		add("standard.version", fmt.Sprintf("unknown schema version %q (this tool supports %q)", p.Standard.Version, SchemaVersion))
	}
	if p.Standard.Name != "" && !namePattern.MatchString(p.Standard.Name) {
		add("standard.name", "must be lowercase alphanumerics and hyphens")
	}

	if p.Repository.GovernanceProfile != "" && !contains(ValidGovernanceProfiles, p.Repository.GovernanceProfile) {
		add("repository.governance_profile", fmt.Sprintf("must be one of %s", strings.Join(ValidGovernanceProfiles, ", ")))
	}
	if p.Repository.Visibility != "" && !contains(ValidVisibilities, p.Repository.Visibility) {
		add("repository.visibility", fmt.Sprintf("must be one of %s", strings.Join(ValidVisibilities, ", ")))
	}
	if p.Repository.DefaultBranch != "" && !defaultBranchPattern.MatchString(p.Repository.DefaultBranch) {
		add("repository.default_branch", "invalid branch name")
	}

	if !p.MergeSquash() && !p.Merge.MergeCommit && !p.Merge.Rebase {
		add("merge", "at least one merge method must be enabled")
	}
	if p.Merge.LinearHistory && p.Merge.MergeCommit {
		add("merge.linear_history", "incompatible with merge_commit")
	}

	if p.Git.CommitStyle != "" && !contains(ValidCommitStyles, p.Git.CommitStyle) {
		add("git.commit_style", fmt.Sprintf("must be one of %s", strings.Join(ValidCommitStyles, ", ")))
	}
	if p.Git.BranchStyle != "" && !contains(ValidBranchStyles, p.Git.BranchStyle) {
		add("git.branch_style", fmt.Sprintf("must be one of %s", strings.Join(ValidBranchStyles, ", ")))
	}

	if p.Security.ActionPinning != "" && !contains(ValidActionPinning, p.Security.ActionPinning) {
		add("security.action_pinning", fmt.Sprintf("must be one of %s", strings.Join(ValidActionPinning, ", ")))
	}
	if p.SecuritySecretScanningPushProtection() && !p.SecuritySecretScanning() {
		add("security.secret_scanning_push_protection", "requires secret_scanning to be enabled")
	}

	if p.Release.Strategy != "" && !contains(ValidReleaseStrategies, p.Release.Strategy) {
		add("release.strategy", fmt.Sprintf("must be one of %s", strings.Join(ValidReleaseStrategies, ", ")))
	}
	if p.Project.Kind != "" && !contains(ValidProjectKinds, p.Project.Kind) {
		add("project.kind", fmt.Sprintf("must be one of %s", strings.Join(ValidProjectKinds, ", ")))
	}

	v = append(v, validateGovernanceOptIn(p)...)
	v = append(v, validateOwnership(p)...)

	return v
}

// validateGovernanceOptIn enforces the two repository features the
// RepoForge governance standard cannot operate without. Declaring a
// governance profile is the opt-in, and an opt-in repository must keep
// Issues (the unit of work, spec.md §12) and the Wiki (the seeded
// documentation surface, spec.md §8.5) enabled. Turning either off is
// a policy error, not a silent downgrade.
func validateGovernanceOptIn(p *Policy) []Violation {
	var v []Violation
	if p.Repository.GovernanceProfile == "" {
		return nil
	}
	if p.GitHub.Issues != nil && !*p.GitHub.Issues {
		v = append(v, Violation{
			Field:   "github.issues",
			Message: "must be true while repository.governance_profile is set (the RepoForge governance standard requires Issues)",
		})
	}
	if !p.GitHub.Wiki {
		v = append(v, Violation{
			Field:   "github.wiki",
			Message: "must be true while repository.governance_profile is set (the RepoForge governance standard requires the wiki; set it explicitly or omit the profile)",
		})
	}
	return v
}

func validateOwnership(p *Policy) []Violation {
	var v []Violation
	seen := map[string]string{}
	lists := []struct {
		field string
		items []string
	}{
		{"ownership.managed_paths", p.Ownership.ManagedPaths},
		{"ownership.scaffolded_paths", p.Ownership.ScaffoldedPaths},
		{"ownership.user_owned_paths", p.Ownership.UserOwnedPaths},
		{"ownership.placeholder_paths", p.Ownership.PlaceholderPaths},
	}
	for _, l := range lists {
		for _, pat := range l.items {
			if pat == "" || strings.HasPrefix(pat, "/") {
				v = append(v, Violation{Field: l.field, Message: fmt.Sprintf("pattern %q must be relative to the repository root", pat)})
				continue
			}
			if err := ValidatePattern(pat); err != nil {
				v = append(v, Violation{Field: l.field, Message: fmt.Sprintf("invalid glob pattern %q: %v", pat, err)})
			}
			if prev, dup := seen[pat]; dup {
				v = append(v, Violation{Field: l.field, Message: fmt.Sprintf("pattern %q already declared in %s", pat, prev)})
			} else {
				seen[pat] = l.field
			}
		}
	}
	return v
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
