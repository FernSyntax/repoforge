// Package doctor audits a repository against the RepoForge policy
// and reports drift. Doctor is strictly read-only (spec.md §11.2): it never
// modifies the local working tree, local git state, or any GitHub
// state — every remote call is a GET. This invariant has
// machine-checkable tests.
package doctor

import (
	"context"
	"fmt"
	"strings"

	"github.com/FernSyntax/repoforge/configs"
	"github.com/FernSyntax/repoforge/internal/config"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// Severity of a single audit finding.
type Severity string

const (
	Pass        Severity = "PASS"
	Warn        Severity = "WARN"
	Fail        Severity = "FAIL"
	Unsupported Severity = "UNSUPPORTED"
)

// Finding is a single audit result. UNSUPPORTED is never a failure: it
// records a capability or plan gap (docs/reference/github-capabilities.md).
type Finding struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Report is the outcome of an audit.
type Report struct {
	Repository string    `json:"repository"`
	Findings   []Finding `json:"findings"`
}

// Summary counts findings by severity.
func (r Report) Summary() (pass, warn, fail, unsupported int) {
	for _, f := range r.Findings {
		switch f.Severity {
		case Pass:
			pass++
		case Warn:
			warn++
		case Fail:
			fail++
		case Unsupported:
			unsupported++
		}
	}
	return
}

// Failed reports whether any finding is a FAIL (drift). WARN and
// UNSUPPORTED do not fail the audit (spec.md §11.4).
func (r Report) Failed() bool {
	for _, f := range r.Findings {
		if f.Severity == Fail {
			return true
		}
	}
	return false
}

// Options control an audit.
type Options struct {
	// StartDir is where repository discovery begins (default ".").
	StartDir string
	// Repo overrides the "owner/name" slug; default parses origin.
	Repo string
	// Token is an explicit credential (tests/embedding); the CLI never
	// sets it — resolution goes through github.ResolveToken (spec.md §8.1).
	Token string
	// Client injects a GitHub client (dependency injection for tests).
	Client *ghub.Client
}

// ExitCode maps audit outcome to spec.md §11.4: 0 = no drift, 1 = drift,
// 2 = audit could not complete.
func ExitCode(rep Report, err error) int {
	if err != nil {
		return 2
	}
	if rep.Failed() {
		return 1
	}
	return 0
}

// Run audits local and remote state. Local findings are always
// produced; if the remote audit cannot be set up (no credentials, no
// slug), Run returns the local report together with the setup error so
// callers can exit 2 after printing it.
func Run(ctx context.Context, opts Options) (Report, error) {
	var rep Report
	root, pol, err := runLocal(&rep, opts.StartDir)
	if err != nil {
		return rep, err
	}

	owner, repoName, err := resolveSlug(opts)
	if err != nil {
		return rep, err
	}
	rep.Repository = owner + "/" + repoName

	client := opts.Client
	if client == nil {
		token, err := ghub.ResolveToken(ctx, opts.Token)
		if err != nil {
			return rep, err
		}
		client = ghub.New(ghub.WithToken(token))
	}

	var wantMain, wantTags *ghub.RulesetPayload
	if pol != nil {
		contexts := config.DiscoverCheckContexts(root)
		if pol.Security.CodeScanning {
			contexts = append(contexts, config.CodeScanningCheckContexts(root)...)
		}
		wantMain, wantTags, err = configs.DesiredRulesets(pol.GovernanceProfileName(), contexts, pol.MergeMethods())
		if err != nil {
			return rep, err
		}
	}

	if err := runRemote(ctx, &rep, client, owner, repoName, pol, wantMain, wantTags); err != nil {
		return rep, err
	}
	return rep, nil
}

func (r *Report) add(sev Severity, check, format string, args ...any) {
	r.Findings = append(r.Findings, Finding{
		Check:    check,
		Severity: sev,
		Message:  fmt.Sprintf(format, args...),
	})
}

func resolveSlug(opts Options) (string, string, error) {
	slug := opts.Repo
	if slug == "" {
		var err error
		slug, err = SlugFromOrigin(opts.StartDir)
		if err != nil {
			return "", "", err
		}
	}
	parts := strings.Split(strings.TrimSuffix(slug, ".git"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("doctor: invalid repository slug %q (want owner/name)", slug)
	}
	return parts[0], parts[1], nil
}
