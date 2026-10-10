package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Repository is the documented "Get a repository" response subset
// relevant to governance: identity, visibility, feature toggles, and
// merge settings. Field names follow the API.
type Repository struct {
	FullName            string `json:"full_name"`
	Description         string `json:"description"`
	Private             bool   `json:"private"`
	Visibility          string `json:"visibility"`
	DefaultBranch       string `json:"default_branch"`
	HasIssues           bool   `json:"has_issues"`
	HasDiscussions      bool   `json:"has_discussions"`
	HasProjects         bool   `json:"has_projects"`
	HasWiki             bool   `json:"has_wiki"`
	AllowSquashMerge    bool   `json:"allow_squash_merge"`
	AllowMergeCommit    bool   `json:"allow_merge_commit"`
	AllowRebaseMerge    bool   `json:"allow_rebase_merge"`
	DeleteBranchOnMerge bool   `json:"delete_branch_on_merge"`
	AllowAutoMerge      bool   `json:"allow_auto_merge"`
	SecurityAndAnalysis struct {
		AdvancedSecurity             *statusField `json:"advanced_security"`
		SecretScanning               *statusField `json:"secret_scanning"`
		SecretScanningPushProtection *statusField `json:"secret_scanning_push_protection"`
	} `json:"security_and_analysis"`
}

type statusField struct {
	Status string `json:"status"`
}

// SecuritySettings aggregates the security posture readable via the
// API "where API support exists" (docs/reference/github-capabilities.md).
type SecuritySettings struct {
	// From the repository object's security_and_analysis (admin-visible;
	// nil when the API does not expose it to the caller).
	AdvancedSecurity             string
	SecretScanning               string
	SecretScanningPushProtection string
	// Probed endpoints.
	DependabotAlertsEnabled       bool
	AutomatedSecurityFixesEnabled bool
	PrivateVulnerabilityReporting bool
	CodeScanningDefaultSetup      string // e.g. "configured", "" when not set up
}

// Ruleset is a repository ruleset list item (documented in the
// repository rules API).
type Ruleset struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Target      string `json:"target"`
	SourceType  string `json:"source_type"`
	Source      string `json:"source"`
	Enforcement string `json:"enforcement"`
}

// RulesetDetail is the "Get a repository ruleset" response, including
// the rules. Parameters are kept raw and decoded per rule type.
//
// BypassActors is a pointer because the API omits the property unless
// the requester has write access to the ruleset (documented, to avoid
// leaking sensitive information): nil means "not reported", never
// "no bypass actors", so drift comparison skips it in that case.
type RulesetDetail struct {
	ID           int                   `json:"id"`
	Name         string                `json:"name"`
	Target       string                `json:"target"`
	SourceType   string                `json:"source_type"`
	Enforcement  string                `json:"enforcement"`
	Conditions   *RulesetConditions    `json:"conditions"`
	BypassActors *[]RulesetBypassActor `json:"bypass_actors"`
	Rules        []struct {
		Type       string          `json:"type"`
		Parameters json.RawMessage `json:"parameters"`
	} `json:"rules"`
}

// PullRequestRuleParams returns the decoded pull_request rule
// parameters, or nil if the rule is absent.
func (d *RulesetDetail) PullRequestRuleParams() *PullRequestRuleParams {
	for _, r := range d.Rules {
		if r.Type == "pull_request" && len(r.Parameters) > 0 {
			var p PullRequestRuleParams
			if err := json.Unmarshal(r.Parameters, &p); err == nil {
				return &p
			}
		}
	}
	return nil
}

// PullRequestRuleParams are the documented parameters of the
// pull_request ruleset rule.
type PullRequestRuleParams struct {
	AllowedMergeMethods            []string `json:"allowed_merge_methods"`
	DismissStaleReviewsOnPush      bool     `json:"dismiss_stale_reviews_on_push"`
	RequireCodeOwnerReview         bool     `json:"require_code_owner_review"`
	RequireLastPushApproval        bool     `json:"require_last_push_approval"`
	RequiredApprovingReviewCount   int      `json:"required_approving_review_count"`
	RequiredReviewThreadResolution bool     `json:"required_review_thread_resolution"`
}

// IssueNumber is the number of an issue or pull request.
type IssueNumber = int

// IssueExists reports whether the given issue number resolves to an
// issue that still exists (GET /repos/{owner}/{repo}/issues/{number}).
// Pull requests share the issues endpoint, so an issue number that now
// belongs to a pull request is reported as existing — which is correct
// for linkage validation, where the intent is "this reference resolves".
func (c *Client) IssueExists(ctx context.Context, owner, repo string, number int) error {
	path, err := repoPath(owner, repo, "issues", strconv.Itoa(number))
	if err != nil {
		return err
	}
	var issue struct {
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	if err := c.get(ctx, path, &issue); err != nil {
		if IsNotFound(err) {
			return fmt.Errorf("github: issue #%d does not exist in %s/%s", number, owner, repo)
		}
		return fmt.Errorf("github: reading issue #%d: %w", number, err)
	}
	return nil
}

// GetRuleset fetches one ruleset with its rules
// (GET /repos/{owner}/{repo}/rulesets/{id}).
func (c *Client) GetRuleset(ctx context.Context, owner, repo string, id int) (*RulesetDetail, error) {
	var d RulesetDetail
	path, err := repoPath(owner, repo, "rulesets", strconv.Itoa(id))
	if err != nil {
		return nil, err
	}
	if err := c.get(ctx, path, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Label is a repository label.
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
}

// NormalizeLabelColor canonicalizes a label color for comparison:
// lowercase hexadecimal without a leading '#', matching the API
// convention for both reads and writes.
func NormalizeLabelColor(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "#"))
}

// Topics is the repository topics response.
type Topics struct {
	Names []string `json:"names"`
}

// GetRepository fetches repository metadata and settings
// (GET /repos/{owner}/{repo}).
func (c *Client) GetRepository(ctx context.Context, owner, repo string) (*Repository, error) {
	var r Repository
	path, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListRulesets lists all repository rulesets, following pagination
// (GET /repos/{owner}/{repo}/rulesets).
func (c *Client) ListRulesets(ctx context.Context, owner, repo string) ([]Ruleset, error) {
	path, err := repoPath(owner, repo, "rulesets")
	if err != nil {
		return nil, err
	}
	return getAll[Ruleset](c, ctx, path)
}

// ListTopics lists repository topics (GET /repos/{owner}/{repo}/topics).
func (c *Client) ListTopics(ctx context.Context, owner, repo string) (*Topics, error) {
	var t Topics
	path, err := repoPath(owner, repo, "topics")
	if err != nil {
		return nil, err
	}
	if err := c.get(ctx, path, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// ListLabels lists all repository labels, following pagination
// (GET /repos/{owner}/{repo}/labels).
func (c *Client) ListLabels(ctx context.Context, owner, repo string) ([]Label, error) {
	path, err := repoPath(owner, repo, "labels")
	if err != nil {
		return nil, err
	}
	return getAll[Label](c, ctx, path)
}

// GetSecuritySettings inspects security posture from the repository
// object's security_and_analysis plus the documented probe endpoints:
// vulnerability-alerts (204 = enabled, 404 = disabled),
// automated-security-fixes, and code-scanning/default-setup.
func (c *Client) GetSecuritySettings(ctx context.Context, owner, repo string) (*SecuritySettings, error) {
	r, err := c.GetRepository(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	s := &SecuritySettings{}
	if r.SecurityAndAnalysis.AdvancedSecurity != nil {
		s.AdvancedSecurity = r.SecurityAndAnalysis.AdvancedSecurity.Status
	}
	if r.SecurityAndAnalysis.SecretScanning != nil {
		s.SecretScanning = r.SecurityAndAnalysis.SecretScanning.Status
	}
	if r.SecurityAndAnalysis.SecretScanningPushProtection != nil {
		s.SecretScanningPushProtection = r.SecurityAndAnalysis.SecretScanningPushProtection.Status
	}

	base, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}

	s.DependabotAlertsEnabled, err = c.probeEnabled(ctx, base+"/vulnerability-alerts")
	if err != nil {
		return nil, fmt.Errorf("github: probing vulnerability alerts: %w", err)
	}

	var fixes struct {
		Enabled bool `json:"enabled"`
	}
	err = c.get(ctx, base+"/automated-security-fixes", &fixes)
	switch {
	case err == nil:
		s.AutomatedSecurityFixesEnabled = fixes.Enabled
	case IsNotFound(err):
		s.AutomatedSecurityFixesEnabled = false
	default:
		return nil, fmt.Errorf("github: probing automated security fixes: %w", err)
	}

	var setup struct {
		State string `json:"state"`
	}
	err = c.get(ctx, base+"/code-scanning/default-setup", &setup)
	switch {
	case err == nil:
		s.CodeScanningDefaultSetup = setup.State
	case IsNotFound(err):
		s.CodeScanningDefaultSetup = ""
	default:
		return nil, fmt.Errorf("github: probing code scanning default setup: %w", err)
	}

	// Private vulnerability reporting: GET returns {"enabled": bool}.
	// A 404 means the state cannot be confirmed (same scope-masking
	// behavior as the other security probes), so it reads as
	// disabled and bootstrap reconciles or fails explicitly.
	var pvr struct {
		Enabled bool `json:"enabled"`
	}
	err = c.get(ctx, base+"/private-vulnerability-reporting", &pvr)
	switch {
	case err == nil:
		s.PrivateVulnerabilityReporting = pvr.Enabled
	case IsNotFound(err):
		s.PrivateVulnerabilityReporting = false
	default:
		return nil, fmt.Errorf("github: probing private vulnerability reporting: %w", err)
	}

	return s, nil
}

// probeEnabled treats 2xx as enabled and 404 as disabled (documented
// behavior of the vulnerability-alerts endpoint).
func (c *Client) probeEnabled(ctx context.Context, path string) (bool, error) {
	_, err := c.do(ctx, http.MethodGet, path, nil)
	if err == nil {
		return true, nil
	}
	if IsNotFound(err) {
		return false, nil
	}
	return false, err
}
