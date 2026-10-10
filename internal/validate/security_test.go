package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Security-focused validation tests: unpinned actions, secret-shaped
// material, and the least-privilege baseline of the repository's own
// workflows (docs/architecture/security.md). Secret-shaped fixtures
// are built at runtime so this source file never trips its own
// detectors (or gitleaks): no literal below matches a secret pattern.

func TestUnpinnedActionFlagged(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".github/workflows/ci.yml",
		"name: CI\non: [push]\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n    steps:\n      - uses: actions/checkout@v4\n")
	commitAll(t, dir, "ci: add workflow")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFinding(rep, "yaml/workflow") {
		t.Fatalf("expected yaml/workflow finding for mutable action ref, got: %+v", rep.Findings)
	}
}

func TestPinnedActionAccepted(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".github/workflows/ci.yml",
		"name: CI\non: [push]\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n    steps:\n      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1\n      - run: echo local\n")
	commitAll(t, dir, "ci: add workflow")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasFinding(rep, "yaml/workflow") {
		t.Fatalf("SHA-pinned workflow must be clean, got: %+v", rep.Findings)
	}
}

func TestLocalActionExempt(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".github/workflows/ci.yml",
		"name: CI\non: [push]\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n    steps:\n      - uses: ./internal/action\n")
	commitAll(t, dir, "ci: add workflow")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasFinding(rep, "yaml/workflow") {
		t.Fatalf("local action must be exempt from pinning, got: %+v", rep.Findings)
	}
}

func TestSecretPatterns(t *testing.T) {
	flagged := map[string]string{
		"GitHub PAT":        "ghp_" + "0123456789abcdef0123456789abcdef0123",
		"fine-grained":      "github_pat_" + "0123456789abcdefghij_1",
		"OAuth token":       "gho_" + "0123456789abcdef0123456789abcdef0123",
		"private key block": "-----BEGIN " + "PRIVATE KEY-----",
		"AWS key ID":        "AKIA" + "IOSFODNN7EXAMPLE",
		"Slack token":       "xoxb-" + "123456789012",
	}
	for name, secret := range flagged {
		t.Run("flagged/"+name, func(t *testing.T) {
			dir := initRepo(t)
			write(t, dir, "leak.txt", "token: "+secret+"\n")
			commitAll(t, dir, "chore: add fixture")
			rep, err := run(t, dir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !hasFinding(rep, "content/secret") {
				t.Fatalf("expected content/secret finding for %s", name)
			}
		})
	}
	clean := map[string]string{
		"env var names": "reads GH_TOKEN, GITHUB_TOKEN, or the gh CLI session",
		"short PAT":     "ghp_" + "tooshort",
		"short AWS key": "AKIA" + "123",
		"angle syntax":  "uses <owner> template syntax",
	}
	for name, content := range clean {
		t.Run("clean/"+name, func(t *testing.T) {
			dir := initRepo(t)
			write(t, dir, "notes.txt", content+"\n")
			commitAll(t, dir, "docs: add fixture")
			rep, err := run(t, dir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hasFinding(rep, "content/secret") {
				t.Fatalf("must not flag %s, got: %+v", name, rep.Findings)
			}
		})
	}
}

// allowedWrites names the only write grants in the baseline, each
// tied to the job that justifies it: SARIF upload to code scanning,
// the release job (publishing the release, minting and storing
// attestations), and the publication job (minting a GitHub App
// installation token, which requires id-token: write — the actual
// content writes are carried by that token, not by the workflow
// grant). Everything else must be read-only.
var allowedWrites = map[string]map[string]bool{
	"scorecard.yml/scorecard": {
		"security-events": true,
	},
	"release.yml/release": {
		"contents":     true,
		"id-token":     true,
		"attestations": true,
	},
	"template-publication.yml/publish": {
		// Minting an App installation token is the only thing this
		// grant is for: the token carries contents, workflows,
		// pull-requests, and checks scoped to the template repository
		// alone, and it is minted only inside this job.
		"id-token": true,
	},
	"template-preflight.yml/preflight": {
		// The preflight's whole purpose is to prove the App private key
		// is valid, and minting a token is the only way to do that.
		"id-token": true,
	},
}

// TestWorkflowPermissionsBaseline audits the repository's own
// workflows against the least-privilege invariant: empty top-level
// permissions, explicit per-job permissions, no write grant except
// the SARIF uploads, no pull_request_target, SHA-pinned actions, and
// credential-less checkouts.
func TestWorkflowPermissionsBaseline(t *testing.T) {
	dir := filepath.Join("..", "..", ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read own workflows (invariant applies to this repository): %v", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && (strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			files = append(files, name)
		}
	}
	if len(files) == 0 {
		t.Fatal("no workflows found; baseline must exist")
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]any
			if err := yaml.Unmarshal(data, &raw); err != nil {
				t.Fatalf("invalid YAML: %v", err)
			}
			perms, present := raw["permissions"]
			if !present {
				t.Fatal("missing top-level permissions (must be present and grant nothing writable)")
			}
			assertNoWrite(t, "top-level", name, "", perms)
			var wf struct {
				On   any `yaml:"on"`
				Jobs map[string]struct {
					Permissions map[string]string `yaml:"permissions"`
					Steps       []struct {
						Uses string         `yaml:"uses"`
						With map[string]any `yaml:"with"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(data, &wf); err != nil {
				t.Fatalf("invalid YAML: %v", err)
			}
			assertNoPullRequestTarget(t, name, wf.On)
			if len(wf.Jobs) == 0 {
				t.Fatal("workflow defines no jobs")
			}
			for jobName, job := range wf.Jobs {
				if job.Permissions == nil {
					t.Errorf("job %q has no explicit permissions block", jobName)
				}
				for scope, level := range job.Permissions {
					if level == "write" || level == "admin" {
						if !allowedWrites[name+"/"+jobName][scope] {
							t.Errorf("job %q grants %s: %s (write grants must be allowlisted in allowedWrites with justification)", jobName, scope, level)
						}
					}
				}
				for _, step := range job.Steps {
					if msg := checkActionPin(step.Uses); msg != "" {
						t.Errorf("job %q: %s", jobName, msg)
					}
					if strings.HasPrefix(step.Uses, "actions/checkout@") {
						v, ok := step.With["persist-credentials"]
						if !ok || v != false {
							t.Errorf("job %q: checkout must set persist-credentials: false", jobName)
						}
					}
				}
			}
		})
	}
}

func assertNoWrite(t *testing.T, where, file, job string, perms any) {
	t.Helper()
	m, ok := perms.(map[string]any)
	if !ok {
		return
	}
	for scope, level := range m {
		if level == "write" || level == "admin" {
			t.Errorf("%s permissions grant %s: %v in %s/%s", where, scope, level, file, job)
		}
	}
}

func assertNoPullRequestTarget(t *testing.T, file string, on any) {
	t.Helper()
	switch v := on.(type) {
	case string:
		if v == "pull_request_target" {
			t.Errorf("%s triggers on pull_request_target", file)
		}
	case []any:
		for _, e := range v {
			assertNoPullRequestTarget(t, file, e)
		}
	case map[string]any:
		for k, e := range v {
			if k == "pull_request_target" {
				t.Errorf("%s triggers on pull_request_target", file)
			}
			assertNoPullRequestTarget(t, file, e)
		}
	}
}
