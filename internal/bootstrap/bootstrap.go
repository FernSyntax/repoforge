// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

// Package bootstrap converges a repository to the RepoForge policy.
//
// Bootstrap is idempotent (spec.md §10.2): running it twice converges to
// the same state and reports zero changes on the second run. It follows
// the plan → confirm → apply → verify cycle (spec.md §10.3) and never
// performs a mutation during planning. Only policy-owned settings are
// touched; unmanaged rulesets and configuration are preserved and
// reported, never overwritten (spec.md §5.3).
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/FernSyntax/repoforge/internal/config"
	"github.com/FernSyntax/repoforge/internal/doctor"
	ghub "github.com/FernSyntax/repoforge/internal/github"
	"github.com/FernSyntax/repoforge/internal/validate"
)

// Action classifies a plan item.
type Action string

const (
	// Applicable mutations.
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionRename  Action = "rename"
	ActionEnable  Action = "enable"
	ActionDisable Action = "disable"
	// Non-mutating outcomes.
	ActionNoop        Action = "no-op"
	ActionReport      Action = "report"
	ActionConflict    Action = "conflict"
	ActionUnsupported Action = "unsupported"
)

// PlanItem is one reconciled setting.
type PlanItem struct {
	Area    string `json:"area"`
	Action  Action `json:"action"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`

	apply func(ctx context.Context) error `json:"-"`
}

// applicable reports whether the item carries a mutation.
func (p PlanItem) applicable() bool {
	switch p.Action {
	case ActionCreate, ActionUpdate, ActionRename, ActionEnable, ActionDisable:
		return true
	}
	return false
}

// Result is the outcome of a bootstrap run.
type Result struct {
	Repository string     `json:"repository"`
	DryRun     bool       `json:"dry_run"`
	Aborted    bool       `json:"aborted"`
	Plan       []PlanItem `json:"plan"`
	Applied    int        `json:"applied"`
	Converged  bool       `json:"converged"`
}

// Options control a bootstrap run.
type Options struct {
	StartDir string
	Repo     string
	Token    string
	DryRun   bool
	Yes      bool
	Client   *ghub.Client
	// Confirm gates application when Yes is false. Nil Confirm with
	// !Yes aborts before any mutation.
	Confirm func(plan []PlanItem) bool
}

// ExitCode: 0 = converged (or nothing to do), 1 = changes pending
// (dry-run), aborted, unsupported capabilities remain, or convergence
// could not be verified; 2 = error.
func ExitCode(res Result, err error) int {
	if err != nil {
		return 2
	}
	if !res.Converged {
		return 1
	}
	return 0
}

// Run executes the full bootstrap cycle: discover, load desired state,
// authenticate, read actual state, plan, (display via caller), confirm,
// apply, re-read, verify convergence.
func Run(ctx context.Context, opts Options) (Result, error) {
	res := Result{DryRun: opts.DryRun}
	if opts.StartDir == "" {
		opts.StartDir = "."
	}
	root, pol, err := loadDesired(opts.StartDir)
	if err != nil {
		return res, err
	}
	owner, repoName, err := resolveSlug(opts)
	if err != nil {
		return res, err
	}
	res.Repository = owner + "/" + repoName

	gov, err := doctor.LoadProfile(pol)
	if err != nil {
		return res, err
	}

	client := opts.Client
	if client == nil {
		token, err := ghub.ResolveToken(ctx, opts.Token)
		if err != nil {
			return res, err
		}
		client = ghub.New(ghub.WithToken(token))
	}

	actual, err := readActual(ctx, client, owner, repoName)
	if err != nil {
		return res, fmt.Errorf("bootstrap: reading current state: %w", err)
	}
	env := &execEnv{client: client, owner: owner, repo: repoName}

	plan := computePlan(root, pol, gov, actual, env)
	res.Plan = plan

	if opts.DryRun {
		res.Converged = !hasApplicable(plan) && !hasUnsupported(plan)
		return res, nil
	}

	if !opts.Yes {
		if opts.Confirm == nil || !opts.Confirm(plan) {
			res.Aborted = true
			return res, nil
		}
	}

	applied, err := applyPlan(ctx, plan)
	res.Applied = applied
	if err != nil {
		return res, err
	}

	// Re-read and re-plan to verify convergence (spec.md §10.2, 10.6).
	after, err := readActual(ctx, client, owner, repoName)
	if err != nil {
		return res, fmt.Errorf("bootstrap: re-reading state after apply: %w", err)
	}
	replan := computePlan(root, pol, gov, after, env)
	res.Plan = replan
	res.Converged = !hasApplicable(replan) && !hasUnsupported(replan)
	return res, nil
}

func hasApplicable(plan []PlanItem) bool {
	for _, p := range plan {
		if p.applicable() {
			return true
		}
	}
	return false
}

func hasUnsupported(plan []PlanItem) bool {
	for _, p := range plan {
		if p.Action == ActionUnsupported {
			return true
		}
	}
	return false
}

// loadDesired discovers the repository root and loads a valid policy;
// bootstrap cannot converge without one (exit-2 class error).
func loadDesired(startDir string) (string, *config.Policy, error) {
	vrep, err := validate.Run(validate.Options{StartDir: startDir})
	if err != nil {
		return "", nil, fmt.Errorf("bootstrap: %v", err)
	}
	if vrep.Policy == "" {
		return "", nil, errors.New("bootstrap: no policy file (repoforge.yaml); nothing to converge to")
	}
	pol, violations, err := config.LoadPolicy(vrep.Policy)
	if err != nil {
		return "", nil, fmt.Errorf("bootstrap: %v", err)
	}
	if len(violations) > 0 {
		return "", nil, fmt.Errorf("bootstrap: policy is invalid (%d violation(s)); run repoforge validate", len(violations))
	}
	root, _ := filepath.Abs(vrep.Root)
	return root, pol, nil
}

func resolveSlug(opts Options) (string, string, error) {
	if opts.Repo != "" {
		return splitSlug(opts.Repo)
	}
	slug, err := doctor.SlugFromOrigin(opts.StartDir)
	if err != nil {
		return "", "", err
	}
	return splitSlug(slug)
}

func splitSlug(slug string) (string, string, error) {
	owner, repoName, ok := strings.Cut(strings.TrimSuffix(slug, ".git"), "/")
	if !ok || owner == "" || repoName == "" {
		return "", "", fmt.Errorf("bootstrap: invalid repository slug %q (want owner/name)", slug)
	}
	return owner, repoName, nil
}
