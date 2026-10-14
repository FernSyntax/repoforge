// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/config"
)

// sha is a syntactically valid full-length commit SHA (40 lowercase hex
// digits). It need not resolve: checkActionPin is a shape check only.
const sha = "0123456789abcdef0123456789abcdef01234567"

// pinPolicy returns a policy that pins actions in the given mode. The
// zero Policy already defaults to "sha" via ActionPinningMode, so only
// the non-default modes need to be set.
func pinPolicy(mode string) *config.Policy {
	p := &config.Policy{}
	p.Security.ActionPinning = mode
	return p
}

// findingsByRule filters findings to one rule.
func findingsByRule(fs []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

// TestCheckActionPin pins the action-pinning rule (spec.md §8.6) as a
// table: the exemptions exist so composite/local actions and Docker
// images are not reported, and every other reference must be a
// full-length lowercase hex digest. A short pin is mutable by its
// author, which is the whole point of pinning.
func TestCheckActionPin(t *testing.T) {
	for _, c := range []struct {
		name string
		uses string
		want string
	}{
		{"empty step", "", ""},
		{"local composite action", "./.github/actions/local", ""},
		{"docker image", "docker://alpine:3.20", ""},
		{"fully pinned", "actions/checkout@" + sha, ""},
		{"unpinned tag", "actions/checkout@v4", `action "actions/checkout@v4" is not pinned to a full-length commit SHA`},
		{"unpinned short sha", "actions/checkout@11bd719", `action "actions/checkout@11bd719" is not pinned to a full-length commit SHA`},
		{"no version at all", "actions/checkout", `action "actions/checkout" is unpinned; pin to a full-length commit SHA`},
		{"uppercase sha", "actions/checkout@" + strings.ToUpper(sha), `action "actions/checkout@` + strings.ToUpper(sha) + `" is not pinned to a full-length commit SHA`},
		{"non-hex sha", "actions/checkout@" + strings.Repeat("z", 40), `action "actions/checkout@` + strings.Repeat("z", 40) + `" is not pinned to a full-length commit SHA`},
		// The last "@" wins, so a sub-action path with a digest in it
		// does not mask the version that is actually unpinned.
		{"subaction path", "github/codeql-action/analyze@v3", `action "github/codeql-action/analyze@v3" is not pinned to a full-length commit SHA`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := checkActionPin(c.uses); got != c.want {
				t.Errorf("checkActionPin(%q) = %q, want %q", c.uses, got, c.want)
			}
		})
	}
}

// TestCheckWorkflowFileStructure covers the structural half of the
// workflow check: a workflow with no trigger or no job is one GitHub
// refuses to run, and it is invisible to actionlint.
func TestCheckWorkflowFileStructure(t *testing.T) {
	for _, c := range []struct {
		name string
		yaml string
		want []string // substrings that must appear
	}{
		{"valid workflow", "on: push\njobs:\n  build:\n    steps:\n      - uses: actions/checkout@" + sha + "\n", nil},
		{"missing on", "jobs:\n  build:\n    steps: []\n", []string{"missing required key: on"}},
		{"missing jobs", "on: push\n", []string{"missing required key: jobs (must define at least one job)"}},
		{"missing both", "name: Empty\n", []string{"missing required key: on", "missing required key: jobs (must define at least one job)"}},
		{
			"unpinned action names its job",
			"on: push\njobs:\n  lint:\n    steps:\n      - uses: actions/setup-go@v5\n",
			[]string{`job "lint"`, "is not pinned to a full-length commit SHA"},
		},
		{
			"several unpinned actions across jobs",
			"on: [push, pull_request]\njobs:\n  a:\n    steps:\n      - uses: actions/checkout@v4\n  b:\n    steps:\n      - uses: ./local\n      - uses: actions/setup-go@v5\n",
			[]string{"actions/checkout@v4", "actions/setup-go@v5"},
		},
		{"invalid yaml", "on: [push\n", []string{"invalid YAML"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := checkWorkflowFile(".github/workflows/ci.yml", []byte(c.yaml), nil)
			for _, want := range c.want {
				var found bool
				for _, f := range got {
					if f.Rule == "yaml/workflow" && strings.Contains(f.Message, want) {
						found = true
					}
				}
				if !found {
					t.Errorf("no finding contains %q; got %+v", want, got)
				}
			}
			if len(c.want) == 0 && len(got) != 0 {
				t.Errorf("expected no findings, got %+v", got)
			}
		})
	}
}

// TestCheckWorkflowFileHonoursPinningPolicy proves the pinning sweep is
// policy-driven: tag and none modes are legitimate opt-outs for
// repositories that pin by ref, and the check must not second-guess
// them. The default (no policy at all) is still "sha".
func TestCheckWorkflowFileHonoursPinningPolicy(t *testing.T) {
	const wf = "on: push\njobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n"
	for _, c := range []struct {
		mode   string
		pol    *config.Policy
		wantIt bool
	}{
		{"default (nil policy)", nil, true},
		{"sha", pinPolicy("sha"), true},
		{"tag", pinPolicy("tag"), false},
		{"none", pinPolicy("none"), false},
	} {
		t.Run(c.mode, func(t *testing.T) {
			got := checkWorkflowFile(".github/workflows/ci.yml", []byte(wf), c.pol)
			if gotIt := len(findingsByRule(got, "yaml/workflow")) > 0; gotIt != c.wantIt {
				t.Errorf("pinning findings = %t, want %t (%+v)", gotIt, c.wantIt, got)
			}
		})
	}
}

// TestCheckSyntaxOnly covers every YAML/JSON file under .github/ that
// is neither a workflow nor an issue form: dependabot.yml,
// PULL_REQUEST_TEMPLATE.md's neighbours, the issue-template config's
// JSON variant. A syntax error there is a file GitHub silently
// ignores, so it has to be caught statically.
func TestCheckSyntaxOnly(t *testing.T) {
	for _, c := range []struct {
		name   string
		data   string
		isJSON bool
		wantIt bool
	}{
		{"valid yaml", "version: 2\nupdates:\n  - package-ecosystem: gomod\n", false, false},
		{"invalid yaml", "version: [2\n", false, true},
		{"valid json", `{"version": 2}`, true, false},
		{"invalid json", `{"version": }`, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := checkSyntaxOnly(".github/dependabot.yml", []byte(c.data), c.isJSON)
			if gotIt := len(got) > 0; gotIt != c.wantIt {
				t.Fatalf("finding = %t, want %t (%+v)", gotIt, c.wantIt, got)
			}
			if c.wantIt {
				if got[0].Rule != "yaml/github-config" {
					t.Errorf("rule = %q, want yaml/github-config", got[0].Rule)
				}
				if !strings.HasPrefix(got[0].Message, "invalid syntax: ") {
					t.Errorf("message = %q, want an invalid-syntax message", got[0].Message)
				}
			}
		})
	}
}

// TestCheckIssueTemplateFile covers the GitHub issue-form schema: the
// keys GitHub treats as required, the title key that must be omitted
// rather than emptied, and the enumerated body types. A form that
// fails any of these renders as "Something went wrong" on the
// new-issue page.
func TestCheckIssueTemplateFile(t *testing.T) {
	const form = "name: Bug\ndescription: Something is broken\nbody:\n  - type: markdown\n    attributes:\n      value: Details\n"
	for _, c := range []struct {
		name string
		yaml string
		want []string
	}{
		{"valid form", form, nil},
		{"missing name", "description: Something is broken\nbody:\n  - type: textarea\n    attributes:\n      label: Details\n", []string{"missing required key: name"}},
		{"missing description", "name: Bug\nbody:\n  - type: textarea\n    attributes:\n      label: Details\n", []string{"missing required key: description"}},
		{"empty title", "name: Bug\ndescription: Broken\ntitle: \"\"\nbody:\n  - type: textarea\n    attributes:\n      label: Details\n", []string{"title must not be empty"}},
		{"omitted title is fine", "name: Bug\ndescription: Broken\nbody:\n  - type: textarea\n    attributes:\n      label: Details\n", nil},
		{"missing body", "name: Bug\ndescription: Broken\n", []string{"missing required key: body"}},
		{"unknown body type", "name: Bug\ndescription: Broken\nbody:\n  - type: markdownn\n    attributes:\n      value: x\n", []string{`body element 0 has missing or unknown type "markdownn"`}},
		{"missing body type", "name: Bug\ndescription: Broken\nbody:\n  - attributes:\n      label: Details\n", []string{`body element 0 has missing or unknown type ""`}},
		{
			"second element reported with its index",
			"name: Bug\ndescription: Broken\nbody:\n  - type: textarea\n    attributes:\n      label: A\n  - type: nope\n    attributes:\n      label: B\n",
			[]string{"body element 1 has missing or unknown type"},
		},
		{"invalid yaml", "name: [Bug\n", []string{"invalid YAML"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := checkIssueTemplateFile(".github/ISSUE_TEMPLATE/bug.yml", []byte(c.yaml))
			for _, want := range c.want {
				var found bool
				for _, f := range got {
					if f.Rule == "yaml/issue-template" && strings.Contains(f.Message, want) {
						found = true
					}
				}
				if !found {
					t.Errorf("no finding contains %q; got %+v", want, got)
				}
			}
			if len(c.want) == 0 && len(got) != 0 {
				t.Errorf("expected no findings, got %+v", got)
			}
		})
	}
}

// TestCheckIssueTemplateConfig covers .github/ISSUE_TEMPLATE/config.y[a]ml,
// which is a different schema from the forms beside it:
// blank_issues_enabled is mandatory and every contact link needs all
// three of name, url and about.
func TestCheckIssueTemplateConfig(t *testing.T) {
	for _, c := range []struct {
		name   string
		file   string
		yaml   string
		want   []string
		absent []string
	}{
		{
			name: "valid config",
			file: "config.yml",
			yaml: "blank_issues_enabled: false\ncontact_links:\n  - name: Chat\n    url: https://example.invalid/chat\n    about: Ask here\n",
		},
		{
			name:   "missing blank_issues_enabled",
			file:   "config.yml",
			yaml:   "contact_links: []\n",
			want:   []string{"missing required key: blank_issues_enabled"},
			absent: []string{"contact_links"},
		},
		{
			name: "contact link missing url",
			file: "config.yaml",
			yaml: "blank_issues_enabled: true\ncontact_links:\n  - name: Chat\n    about: Ask here\n",
			want: []string{"contact_links[0] requires name, url, and about"},
		},
		{
			name: "second contact link reported with its index",
			file: "config.yaml",
			yaml: "blank_issues_enabled: true\ncontact_links:\n" +
				"  - name: Chat\n    url: https://example.invalid/chat\n    about: Ask here\n" +
				"  - about: Broken\n",
			want: []string{"contact_links[1] requires name, url, and about"},
		},
		{
			name: "invalid yaml",
			file: "config.yml",
			yaml: "blank_issues_enabled: [true\n",
			want: []string{"invalid YAML"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := ".github/ISSUE_TEMPLATE/" + c.file
			got := checkIssueTemplateFile(path, []byte(c.yaml))
			for _, want := range c.want {
				var found bool
				for _, f := range got {
					if f.Rule == "yaml/issue-template" && strings.Contains(f.Message, want) {
						found = true
					}
				}
				if !found {
					t.Errorf("no finding contains %q; got %+v", want, got)
				}
			}
			for _, absent := range c.absent {
				for _, f := range got {
					if strings.Contains(f.Message, absent) {
						t.Errorf("unexpected finding mentioning %q: %+v", absent, got)
					}
				}
			}
			if len(c.want) == 0 && len(got) != 0 {
				t.Errorf("expected no findings, got %+v", got)
			}
		})
	}
}

// TestCheckGitHubDirDispatch proves the dispatcher routes each tracked
// file to the right rule set: forms, workflows and everything else are
// different schemas, and a file that is not YAML/JSON at all (a
// markdown template, a shell script under .github/) is not parsed.
func TestCheckGitHubDirDispatch(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".github/ISSUE_TEMPLATE/bug.yml", "name: Bug\ndescription: Broken\nbody:\n  - type: textarea\n    attributes:\n      label: Details\n")
	write(t, dir, ".github/ISSUE_TEMPLATE/config.yml", "blank_issues_enabled: false\n")
	write(t, dir, ".github/ISSUE_TEMPLATE/broken.json", `{"blank_issues_enabled": }`)
	write(t, dir, ".github/workflows/ci.yml", "on: push\njobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n")
	write(t, dir, ".github/workflows/report.json", `{"name": "report"}`)
	write(t, dir, ".github/dependabot.yml", "version: [2\n")
	write(t, dir, ".github/PULL_REQUEST_TEMPLATE.md", "# PR\n")
	write(t, dir, "docs/guide.md", "# Not under .github\n")
	write(t, dir, "README.md", "# Root\n")

	tracked := []string{
		".github/ISSUE_TEMPLATE/bug.yml",
		".github/ISSUE_TEMPLATE/config.yml",
		".github/ISSUE_TEMPLATE/broken.json",
		".github/workflows/ci.yml",
		".github/workflows/report.json",
		".github/dependabot.yml",
		".github/PULL_REQUEST_TEMPLATE.md",
		"docs/guide.md",
		"README.md",
	}
	got := checkGitHubDir(dir, tracked, nil)

	// A well-formed form and config produce nothing at all.
	for _, f := range got {
		if strings.Contains(f.Path, "ISSUE_TEMPLATE/bug.yml") || strings.Contains(f.Path, "ISSUE_TEMPLATE/config.yml") {
			t.Errorf("well-formed file reported: %+v", f)
		}
	}
	for _, c := range []struct{ path, rule, want string }{
		// .json beside the forms is syntax-only, so the form schema
		// must not be applied to it.
		{".github/ISSUE_TEMPLATE/broken.json", "yaml/github-config", "invalid syntax"},
		// .json beside the workflows is syntax-only too, so its
		// missing on/jobs must not be reported.
		{".github/workflows/ci.yml", "yaml/workflow", "not pinned to a full-length commit SHA"},
		{".github/dependabot.yml", "yaml/github-config", "invalid syntax"},
	} {
		var found bool
		for _, f := range got {
			if f.Path == c.path && f.Rule == c.rule && strings.Contains(f.Message, c.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %s/%s %q; got %+v", c.path, c.rule, c.want, got)
		}
	}
	for _, path := range []string{".github/workflows/report.json", ".github/PULL_REQUEST_TEMPLATE.md", "docs/guide.md", "README.md"} {
		for _, f := range got {
			if f.Path == path {
				t.Errorf("unexpected finding for %s: %+v", path, f)
			}
		}
	}
}

// TestCheckGitHubDirReportsUnreadableFile covers the only way a tracked
// path can fail to read: the file was removed between the git listing
// and the check (a concurrent checkout, a shallow clone, a
// partially-written worktree). Reporting it beats silently skipping a
// file that policy expects to be valid.
func TestCheckGitHubDirReportsUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	got := checkGitHubDir(dir, []string{".github/workflows/ghost.yml", ".github/ISSUE_TEMPLATE/ghost.yml"}, nil)
	if len(got) != 2 {
		t.Fatalf("findings = %d, want 2 (%+v)", len(got), got)
	}
	for _, f := range got {
		if f.Rule != "yaml/github-config" {
			t.Errorf("rule = %q, want yaml/github-config", f.Rule)
		}
		if !strings.HasPrefix(f.Message, "cannot read file: ") {
			t.Errorf("message = %q, want a cannot-read message", f.Message)
		}
	}
}

// TestCheckGitHubDirIgnoresOtherPaths keeps the scan scoped: a file
// outside .github/ is checked by other rules, and re-checking it here
// would double-report the same defect.
func TestCheckGitHubDirIgnoresOtherPaths(t *testing.T) {
	dir := t.TempDir()
	got := checkGitHubDir(dir, []string{"README.md", filepath.Join("docs", "x.yml"), ".githubx/thing.yml", ".github/notes.txt"}, nil)
	if len(got) != 0 {
		t.Errorf("expected no findings, got %+v", got)
	}
}
