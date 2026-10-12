// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// This file contains the client's mutation methods. Every method here
// performs a single, non-retried request and is only ever invoked by
// bootstrap after plan display and confirmation (spec.md §10). Doctor and
// validate never call them.

// RepoSettingsPatch carries mutable repository settings. Only set
// (non-nil) fields are sent.
type RepoSettingsPatch struct {
	HasIssues           *bool `json:"has_issues,omitempty"`
	HasDiscussions      *bool `json:"has_discussions,omitempty"`
	HasProjects         *bool `json:"has_projects,omitempty"`
	HasWiki             *bool `json:"has_wiki,omitempty"`
	AllowSquashMerge    *bool `json:"allow_squash_merge,omitempty"`
	AllowMergeCommit    *bool `json:"allow_merge_commit,omitempty"`
	AllowRebaseMerge    *bool `json:"allow_rebase_merge,omitempty"`
	DeleteBranchOnMerge *bool `json:"delete_branch_on_merge,omitempty"`
	AllowAutoMerge      *bool `json:"allow_auto_merge,omitempty"`

	SecurityAndAnalysis *SecurityAndAnalysisPatch `json:"security_and_analysis,omitempty"`
}

// SecurityAndAnalysisPatch toggles security features via the repository
// update endpoint (documented statuses "enabled"/"disabled").
type SecurityAndAnalysisPatch struct {
	SecretScanning               *StatusPatch `json:"secret_scanning,omitempty"`
	SecretScanningPushProtection *StatusPatch `json:"secret_scanning_push_protection,omitempty"`
}

// StatusPatch is a {"status": "..."} toggle.
type StatusPatch struct {
	Status string `json:"status"`
}

// RulesetPayload is the create/update body for repository rulesets
// (POST /repos/{owner}/{repo}/rulesets and PUT .../rulesets/{id}).
// Field names follow the documented request schema exactly.
type RulesetPayload struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Enforcement string `json:"enforcement"`
	// BypassActors is always sent (no omitempty) so updates clear
	// actors added by hand instead of leaving them untouched.
	BypassActors []RulesetBypassActor `json:"bypass_actors"`
	Conditions   *RulesetConditions   `json:"conditions,omitempty"`
	Rules        []RulesetRule        `json:"rules"`
}

// RulesetBypassActor is a documented bypass actor. RepoForge
// payloads carry an empty list: no actor bypasses a RepoForge-owned
// ruleset, including repository administrators. An empty list is sent
// explicitly on create and update so an actor added by hand is cleared
// by the next bootstrap apply — the ruleset is wholly owned (spec.md §5.2).
type RulesetBypassActor struct {
	// ActorID is nullable: documented null for DeployKey and ignored
	// for OrganizationAdmin.
	ActorID    *int   `json:"actor_id,omitempty"`
	ActorType  string `json:"actor_type"`
	BypassMode string `json:"bypass_mode,omitempty"`
}

// RulesetConditions scopes a ruleset to refs (documented
// "Repository ruleset conditions for ref names").
type RulesetConditions struct {
	RefName RulesetRefName `json:"ref_name"`
}

// RulesetRefName is the include/exclude ref pattern set. include
// accepts full ref patterns plus the ~DEFAULT_BRANCH and ~ALL
// shorthands.
type RulesetRefName struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

// RulesetRule is one rule in a ruleset payload.
type RulesetRule struct {
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// mutate performs a single non-idempotent request with a JSON body.
// Mutations are never retried: a retried mutation can double-apply, and
// a retry after an ambiguous transport failure would leave the operator
// unable to tell whether the change landed. An uncertain mutation
// surfaces as an error naming that uncertainty.
func (c *Client) mutate(ctx context.Context, method, path string, body, out any) error {
	var rdr *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("github: encoding request body: %w", err)
		}
		rdr = bytes.NewReader(data)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("github: building request: %w", err)
	}
	req.Header.Set("Accept", acceptMediaType)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// No response was read, so the mutation's effect is unknown.
		// Report the uncertainty rather than implying either outcome.
		return fmt.Errorf("github: %s %s failed and the mutation outcome is UNKNOWN (%v); re-read the actual state with `repoforge doctor` before re-applying", method, path, err)
	}
	_, err = c.decode(resp, out)
	return err
}

// UpdateRepository patches repository settings
// (PATCH /repos/{owner}/{repo}).
func (c *Client) UpdateRepository(ctx context.Context, owner, repo string, patch RepoSettingsPatch) error {
	path, err := repoPath(owner, repo)
	if err != nil {
		return err
	}
	return c.mutate(ctx, http.MethodPatch, path, patch, nil)
}

// ReplaceTopics replaces all repository topics
// (PUT /repos/{owner}/{repo}/topics). Only called when topics are
// policy-managed; never used to clear user-owned topics.
func (c *Client) ReplaceTopics(ctx context.Context, owner, repo string, names []string) error {
	path, err := repoPath(owner, repo, "topics")
	if err != nil {
		return err
	}
	return c.mutate(ctx, http.MethodPut, path, map[string]any{"names": names}, nil)
}

// RenameBranch renames a branch
// (POST /repos/{owner}/{repo}/branches/{branch}/rename).
func (c *Client) RenameBranch(ctx context.Context, owner, repo, branch, newName string) error {
	path, err := branchPath(owner, repo, branch, newName)
	if err != nil {
		return err
	}
	return c.mutate(ctx, http.MethodPost, path, map[string]any{"new_name": newName}, nil)
}

// CreateRuleset creates a repository ruleset and returns the created
// detail (POST /repos/{owner}/{repo}/rulesets).
func (c *Client) CreateRuleset(ctx context.Context, owner, repo string, p RulesetPayload) (*RulesetDetail, error) {
	var d RulesetDetail
	path, err := repoPath(owner, repo, "rulesets")
	if err != nil {
		return nil, err
	}
	if err := c.mutate(ctx, http.MethodPost, path, p, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// UpdateRuleset replaces a repository ruleset
// (PUT /repos/{owner}/{repo}/rulesets/{id}).
func (c *Client) UpdateRuleset(ctx context.Context, owner, repo string, id int, p RulesetPayload) error {
	path, err := repoPath(owner, repo, "rulesets", strconv.Itoa(id))
	if err != nil {
		return err
	}
	return c.mutate(ctx, http.MethodPut, path, p, nil)
}

// LabelPayload is the create/update body for repository labels
// (POST /repos/{owner}/{repo}/labels and PATCH .../labels/{name}).
// Field names follow the documented request schema exactly: color is
// hexadecimal without a leading '#', description is at most 100
// characters. RepoForge never renames labels, so new_name is
// intentionally absent.
type LabelPayload struct {
	Name        string `json:"name,omitempty"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// CreateLabel creates a repository label
// (POST /repos/{owner}/{repo}/labels).
func (c *Client) CreateLabel(ctx context.Context, owner, repo string, p LabelPayload) error {
	path, err := repoPath(owner, repo, "labels")
	if err != nil {
		return err
	}
	return c.mutate(ctx, http.MethodPost, path, p, nil)
}

// UpdateLabel replaces a repository label's color and description
// (PATCH /repos/{owner}/{repo}/labels/{name}). Labels are addressed
// by current name and never renamed. There is deliberately no delete
// method: RepoForge never deletes labels.
func (c *Client) UpdateLabel(ctx context.Context, owner, repo, name string, p LabelPayload) error {
	path, err := labelPath(owner, repo, name)
	if err != nil {
		return err
	}
	return c.mutate(ctx, http.MethodPatch, path, p, nil)
}

// SetVulnerabilityAlerts enables or disables Dependabot alerts
// (PUT|DELETE /repos/{owner}/{repo}/vulnerability-alerts).
func (c *Client) SetVulnerabilityAlerts(ctx context.Context, owner, repo string, enabled bool) error {
	method := http.MethodPut
	if !enabled {
		method = http.MethodDelete
	}
	path, err := repoPath(owner, repo, "vulnerability-alerts")
	if err != nil {
		return err
	}
	return c.mutate(ctx, method, path, nil, nil)
}

// SetAutomatedSecurityFixes enables or disables Dependabot security
// updates (PUT|DELETE /repos/{owner}/{repo}/automated-security-fixes).
func (c *Client) SetAutomatedSecurityFixes(ctx context.Context, owner, repo string, enabled bool) error {
	method := http.MethodPut
	if !enabled {
		method = http.MethodDelete
	}
	path, err := repoPath(owner, repo, "automated-security-fixes")
	if err != nil {
		return err
	}
	return c.mutate(ctx, method, path, nil, nil)
}

// SetPrivateVulnerabilityReporting enables or disables private
// vulnerability reporting (PUT|DELETE
// /repos/{owner}/{repo}/private-vulnerability-reporting, both 204).
func (c *Client) SetPrivateVulnerabilityReporting(ctx context.Context, owner, repo string, enabled bool) error {
	method := http.MethodPut
	if !enabled {
		method = http.MethodDelete
	}
	path, err := repoPath(owner, repo, "private-vulnerability-reporting")
	if err != nil {
		return err
	}
	return c.mutate(ctx, method, path, nil, nil)
}

// SetSecretScanning toggles secret scanning and/or push protection via
// the repository update endpoint's security_and_analysis object.
func (c *Client) SetSecretScanning(ctx context.Context, owner, repo string, scanning, pushProtection bool) error {
	status := func(b bool) *StatusPatch {
		if b {
			return &StatusPatch{Status: "enabled"}
		}
		return &StatusPatch{Status: "disabled"}
	}
	patch := RepoSettingsPatch{
		SecurityAndAnalysis: &SecurityAndAnalysisPatch{
			SecretScanning:               status(scanning),
			SecretScanningPushProtection: status(pushProtection),
		},
	}
	path, err := repoPath(owner, repo)
	if err != nil {
		return err
	}
	return c.mutate(ctx, http.MethodPatch, path, patch, nil)
}

// ConfigureCodeScanningDefaultSetup enables CodeQL default setup via
// the documented update endpoint (PATCH
// /repos/{owner}/{repo}/code-scanning/default-setup), which upserts:
// {"state": "configured"} enables, and the call is idempotent. There
// is no POST create endpoint — a POST fails with 404 (observed
// against the live API).
func (c *Client) ConfigureCodeScanningDefaultSetup(ctx context.Context, owner, repo string, languages []string) error {
	body := map[string]any{"state": "configured"}
	if len(languages) > 0 {
		body["languages"] = languages
	}
	path, err := repoPath(owner, repo, "code-scanning", "default-setup")
	if err != nil {
		return err
	}
	err = c.mutate(ctx, http.MethodPatch, path, body, nil)
	if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 404 {
		return fmt.Errorf("github: code scanning default setup returned 404: GitHub reports Not Found when the configuration cannot be accessed — check that the token may manage code scanning (classic scope: repo) and that code scanning is available to the repository — then re-run bootstrap (it is idempotent): %w", err)
	}
	return err
}

// ListLanguages returns the repository's language breakdown
// (GET /repos/{owner}/{repo}/languages).
func (c *Client) ListLanguages(ctx context.Context, owner, repo string) (map[string]int, error) {
	var langs map[string]int
	if err := c.get(ctx, repoPathOrPanic(owner, repo, "languages"), &langs); err != nil {
		return nil, err
	}
	return langs, nil
}
