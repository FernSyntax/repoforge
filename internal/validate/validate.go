// Package validate checks a local repository checkout against the
// RepoForge standard. It is deterministic (spec.md §12.2): no network,
// no clock, no randomness; identical checkouts produce identical
// findings on every machine.
package validate

import (
	"fmt"
	"sort"

	"github.com/FernSyntax/repoforge/internal/config"
)

// Finding is a single validation violation.
type Finding struct {
	Rule    string `json:"rule"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Report is the outcome of a validation run.
type Report struct {
	Root     string    `json:"root"`
	Policy   string    `json:"policy"`
	Findings []Finding `json:"findings"`
}

// Valid reports whether the run found no violations.
func (r Report) Valid() bool { return len(r.Findings) == 0 }

// Options control a validation run.
type Options struct {
	// StartDir is where repository root discovery begins.
	StartDir string
	// MaxCommits is the number of recent commits inspected for commit
	// message policy. Values <= 0 select the default (20).
	MaxCommits int
}

// FatalError is an invocation or configuration error (exit code 2):
// not a git repository, git unavailable, unreadable/malformed policy.
type FatalError struct {
	msg string
}

func (e *FatalError) Error() string { return e.msg }

func fatal(format string, args ...any) *FatalError {
	return &FatalError{msg: fmt.Sprintf(format, args...)}
}

// ExitCode maps a run outcome to the documented exit codes:
// 0 = valid, 1 = validation failure, 2 = invocation/configuration error.
func ExitCode(rep Report, err error) int {
	if err != nil {
		return 2
	}
	if rep.Valid() {
		return 0
	}
	return 1
}

// Run validates the repository containing opts.StartDir. It returns a
// Report; err is non-nil only for invocation/configuration errors
// (*FatalError), never for ordinary violations.
func Run(opts Options) (Report, error) {
	rep := Report{Findings: []Finding{}}
	if opts.MaxCommits <= 0 {
		opts.MaxCommits = 20
	}
	if opts.StartDir == "" {
		opts.StartDir = "."
	}
	root, err := findRoot(opts.StartDir)
	if err != nil {
		return rep, err
	}
	rep.Root = root
	if err := requireGit(); err != nil {
		return rep, err
	}
	tracked, err := trackedFiles(root)
	if err != nil {
		return rep, err
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, f := range tracked {
		trackedSet[f] = true
	}

	var pol *config.Policy
	policyPath := config.FindPolicyFile(root)
	if policyPath == "" {
		rep.add("config/missing", "", "no RepoForge policy file found (expected one of repoforge.yaml, repoforge.yml, repoforge.json at the repository root)")
	} else {
		rep.Policy = policyPath
		p, violations, err := config.LoadPolicy(policyPath)
		if err != nil {
			return rep, fatal("cannot read policy: %v", err)
		}
		for _, vio := range violations {
			rep.add("config/schema", rel(root, policyPath), vio.String())
		}
		pol = p
	}

	rep.Findings = append(rep.Findings, checkRequiredFiles(trackedSet)...)
	if pol != nil {
		rep.Findings = append(rep.Findings, checkOwnership(pol, tracked)...)
	}
	rep.Findings = append(rep.Findings, checkBranch(root, pol)...)
	rep.Findings = append(rep.Findings, checkCommits(root, opts.MaxCommits, pol)...)
	rep.Findings = append(rep.Findings, checkGitHubDir(root, tracked, pol)...)
	rep.Findings = append(rep.Findings, checkContent(root, tracked, pol)...)
	rep.Findings = append(rep.Findings, checkMarkdownLinks(root, tracked)...)
	rep.Findings = append(rep.Findings, checkDecisions(root, trackedSet)...)
	rep.Findings = append(rep.Findings, checkAgentFiles(root, trackedSet)...)

	sort.Slice(rep.Findings, func(i, j int) bool {
		a, b := rep.Findings[i], rep.Findings[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
	return rep, nil
}

func (r *Report) add(rule, path, message string) {
	r.Findings = append(r.Findings, Finding{Rule: rule, Path: path, Message: message})
}
