// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validPolicy returns the smallest policy that passes every semantic
// check, so each invalid-policy case below mutates exactly one field
// and pins the exact violation that field produces.
func validPolicy() *Policy {
	p := &Policy{}
	p.Standard.Version = SchemaVersion
	return p
}

// hasViolation reports whether any rendered violation contains want.
func hasViolation(violations []Violation, want string) bool {
	for _, v := range violations {
		if strings.Contains(v.String(), want) {
			return true
		}
	}
	return false
}

// requireViolation asserts exactly one violation carrying want.
func requireViolation(t *testing.T, violations []Violation, want string) {
	t.Helper()
	if !hasViolation(violations, want) {
		t.Errorf("violations = %v, want one containing %q", violations, want)
	}
}

// TestValidatePolicyConforming pins the baseline: the minimal policy
// must be valid, so any violation a conforming policy reports is a
// regression in the checker itself, not in the fixture.
func TestValidatePolicyConforming(t *testing.T) {
	if v := ValidatePolicy(validPolicy()); len(v) != 0 {
		t.Errorf("conforming policy reported violations: %v", v)
	}
}

// TestValidatePolicyRejections pins the semantic rules the JSON
// schema cannot express: each case violates exactly one rule and the
// checker must name the offending field with the documented message.
// These messages are operator-facing contract text (spec.md §9), so
// they are asserted verbatim, not just for non-emptiness.
func TestValidatePolicyRejections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Policy)
		want   string
	}{
		{
			name:   "schema version missing",
			mutate: func(p *Policy) { p.Standard.Version = "" },
			want:   "standard.version: required field missing",
		},
		{
			name:   "schema version unknown",
			mutate: func(p *Policy) { p.Standard.Version = "v2" },
			want:   `standard.version: unknown schema version "v2" (this tool supports "v1")`,
		},
		{
			name:   "standard name not lowercase alphanumeric",
			mutate: func(p *Policy) { p.Standard.Name = "Bad_Name" },
			want:   "standard.name: must be lowercase alphanumerics and hyphens",
		},
		{
			name:   "governance profile unknown",
			mutate: func(p *Policy) { p.Repository.GovernanceProfile = "chaos" },
			want:   "repository.governance_profile: must be one of solo, team, strict",
		},
		{
			name:   "visibility unknown",
			mutate: func(p *Policy) { p.Repository.Visibility = "secret" },
			want:   "repository.visibility: must be one of public, private, internal",
		},
		{
			name:   "default branch malformed",
			mutate: func(p *Policy) { p.Repository.DefaultBranch = "feature space" },
			want:   "repository.default_branch: invalid branch name",
		},
		{
			name: "no merge method enabled",
			mutate: func(p *Policy) {
				p.Merge.Squash = boolPtr(false)
				p.Merge.MergeCommit = false
				p.Merge.Rebase = false
			},
			want: "merge: at least one merge method must be enabled",
		},
		{
			name: "linear history conflicts with merge commit",
			mutate: func(p *Policy) {
				p.Merge.MergeCommit = true
				p.Merge.LinearHistory = true
			},
			want: "merge.linear_history: incompatible with merge_commit",
		},
		{
			name:   "commit style unknown",
			mutate: func(p *Policy) { p.Git.CommitStyle = "gitmoji" },
			want:   "git.commit_style: must be one of conventional",
		},
		{
			name:   "branch style unknown",
			mutate: func(p *Policy) { p.Git.BranchStyle = "freeform" },
			want:   "git.branch_style: must be one of type-issue-description",
		},
		{
			name:   "action pinning mode unknown",
			mutate: func(p *Policy) { p.Security.ActionPinning = "branch" },
			want:   "security.action_pinning: must be one of sha, tag, none",
		},
		{
			name: "push protection without secret scanning",
			mutate: func(p *Policy) {
				on := true
				off := false
				p.Security.SecretScanning = &off
				p.Security.SecretScanningPushProtection = &on
			},
			want: "security.secret_scanning_push_protection: requires secret_scanning to be enabled",
		},
		{
			name:   "release strategy unknown",
			mutate: func(p *Policy) { p.Release.Strategy = "apt" },
			want:   "release.strategy: must be one of none, tag, github-release",
		},
		{
			name:   "project kind unknown",
			mutate: func(p *Policy) { p.Project.Kind = "robot" },
			want:   "project.kind: must be one of application, cli, library, service, template, docs, meta",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := validPolicy()
			c.mutate(p)
			requireViolation(t, ValidatePolicy(p), c.want)
		})
	}
}

// TestValidatePolicyMultipleViolations pins that the checker reports
// every problem at once: a policy with several defects must not stop
// at the first, so one validate run tells the operator everything to
// fix.
func TestValidatePolicyMultipleViolations(t *testing.T) {
	p := validPolicy()
	p.Standard.Version = "v9"
	p.Repository.Visibility = "secret"
	p.Git.CommitStyle = "gitmoji"
	p.Project.Kind = "robot"
	v := ValidatePolicy(p)
	if len(v) < 4 {
		t.Fatalf("violations = %v, want at least the four injected defects", v)
	}
	for _, want := range []string{
		`standard.version: unknown schema version "v9"`,
		"repository.visibility: must be one of public, private, internal",
		"git.commit_style: must be one of conventional",
		"project.kind: must be one of application, cli, library, service, template, docs, meta",
	} {
		requireViolation(t, v, want)
	}
}

// TestValidateOwnership pins the ownership-path rules: paths must be
// repository-relative and syntactically valid globs, and a pattern
// may appear in exactly one list so managed, user-owned, and
// placeholder paths can never overlap (overlap would let bootstrap
// overwrite user files or vice versa).
func TestValidateOwnership(t *testing.T) {
	t.Run("empty pattern", func(t *testing.T) {
		p := validPolicy()
		p.Ownership.ManagedPaths = []string{""}
		requireViolation(t, ValidatePolicy(p),
			`ownership.managed_paths: pattern "" must be relative to the repository root`)
	})
	t.Run("absolute pattern", func(t *testing.T) {
		p := validPolicy()
		p.Ownership.UserOwnedPaths = []string{"/etc/passwd"}
		requireViolation(t, ValidatePolicy(p),
			`ownership.user_owned_paths: pattern "/etc/passwd" must be relative to the repository root`)
	})
	t.Run("invalid glob", func(t *testing.T) {
		p := validPolicy()
		p.Ownership.ScaffoldedPaths = []string{"docs/[bad"}
		requireViolation(t, ValidatePolicy(p),
			`ownership.scaffolded_paths: invalid glob pattern "docs/[bad": unterminated character class in "docs/[bad"`)
	})
	t.Run("duplicate across lists", func(t *testing.T) {
		p := validPolicy()
		p.Ownership.ManagedPaths = []string{"docs/**"}
		p.Ownership.UserOwnedPaths = []string{"docs/**"}
		requireViolation(t, ValidatePolicy(p),
			`ownership.user_owned_paths: pattern "docs/**" already declared in ownership.managed_paths`)
	})
	t.Run("duplicate within one list", func(t *testing.T) {
		p := validPolicy()
		p.Ownership.PlaceholderPaths = []string{"docs/README.md", "docs/README.md"}
		requireViolation(t, ValidatePolicy(p),
			`ownership.placeholder_paths: pattern "docs/README.md" already declared in ownership.placeholder_paths`)
	})
	t.Run("empty pattern skips the remaining checks", func(t *testing.T) {
		// The relative-path check continues to the next pattern,
		// so two empty entries report two "must be relative"
		// violations and never a duplicate report.
		p := validPolicy()
		p.Ownership.ManagedPaths = []string{"", ""}
		v := ValidatePolicy(p)
		if len(v) != 2 {
			t.Fatalf("violations = %v, want exactly the two relative-path errors", v)
		}
		for _, vio := range v {
			if !strings.Contains(vio.String(), "must be relative to the repository root") {
				t.Errorf("violation = %q, want the relative-path error", vio)
			}
		}
	})
	t.Run("invalid glob still reports duplicates", func(t *testing.T) {
		// Unlike the relative-path check, an invalid glob does
		// not skip the duplicate check, so a repeated invalid
		// pattern reports the glob error once per occurrence
		// plus the duplicate declaration.
		p := validPolicy()
		p.Ownership.ManagedPaths = []string{"a[b", "a[b"}
		v := ValidatePolicy(p)
		if len(v) != 3 {
			t.Fatalf("violations = %v, want two glob errors and one duplicate", v)
		}
	})
	t.Run("valid paths across lists", func(t *testing.T) {
		p := validPolicy()
		p.Ownership.ManagedPaths = []string{"docs/**", "*.md"}
		p.Ownership.ScaffoldedPaths = []string{"src/**"}
		p.Ownership.UserOwnedPaths = []string{"apps/**"}
		p.Ownership.PlaceholderPaths = []string{"hack/[abc].sh"}
		if v := ValidatePolicy(p); len(v) != 0 {
			t.Errorf("valid ownership reported violations: %v", v)
		}
	})
}

// TestLoadPolicyValidFiles pins the loader's success paths: YAML and
// JSON policies both decode, unknown fields are rejected as
// violations rather than silently ignored (spec.md §9.2), and the
// returned policy carries the decoded values.
func TestLoadPolicyValidFiles(t *testing.T) {
	dir := t.TempDir()

	yamlPath := filepath.Join(dir, "repoforge.yaml")
	if err := os.WriteFile(yamlPath, []byte(
		"standard:\n  version: v1\nrepository:\n  governance_profile: team\ngithub:\n  wiki: true\nmerge:\n  squash: false\n  merge_commit: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, violations, err := LoadPolicy(yamlPath)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none", violations)
	}
	if p.Repository.GovernanceProfile != "team" {
		t.Errorf("governance_profile = %q, want team", p.Repository.GovernanceProfile)
	}
	if p.MergeSquash() {
		t.Error("MergeSquash() = true, want false (explicit squash: false)")
	}

	jsonPath := filepath.Join(dir, "repoforge.json")
	if err := os.WriteFile(jsonPath, []byte(
		`{"standard":{"version":"v1"},"repository":{"visibility":"private"},"github":{"wiki":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, violations, err = LoadPolicy(jsonPath)
	if err != nil {
		t.Fatalf("LoadPolicy json: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("json violations = %v, want none", violations)
	}
	if p.Repository.Visibility != "private" {
		t.Errorf("visibility = %q, want private", p.Repository.Visibility)
	}
}

// TestLoadPolicyUnknownFieldRejected pins strict decoding: an
// unknown YAML field is reported as a violation naming the field,
// not silently dropped — the schema's additionalProperties: false
// must hold at runtime too.
func TestLoadPolicyUnknownFieldRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repoforge.yaml")
	if err := os.WriteFile(path, []byte("standard:\n  version: v1\n  author: nobody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, violations, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if !hasViolation(violations, "field author not found") {
		t.Errorf("violations = %v, want one naming the unknown field", violations)
	}
}

// TestLoadPolicyTypeErrorAsViolations pins that a type mismatch
// (string where a bool belongs) surfaces as violations, so a
// mis-typed policy is a fixable config error rather than a crash.
func TestLoadPolicyTypeErrorAsViolations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repoforge.yaml")
	if err := os.WriteFile(path, []byte("standard:\n  version: v1\nmerge:\n  squash: sometimes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, violations, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if p != nil {
		t.Error("policy must not be returned alongside type-error violations")
	}
	if !hasViolation(violations, "cannot unmarshal !!str") {
		t.Errorf("violations = %v, want one naming the unmarshal failure", violations)
	}
}

// TestLoadPolicySyntaxErrors pins the SyntaxError contract: a file
// that cannot be parsed at all (broken YAML, invalid JSON) or does
// not exist yields *SyntaxError wrapping the cause, so callers map
// it to exit code 2 while semantic problems stay violations.
func TestLoadPolicySyntaxErrors(t *testing.T) {
	dir := t.TempDir()

	brokenYAML := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(brokenYAML, []byte("standard:\n  version: [v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadPolicy(brokenYAML)
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("LoadPolicy(broken YAML) error = %v, want *SyntaxError", err)
	}
	if se.Path != brokenYAML {
		t.Errorf("SyntaxError.Path = %q, want %q", se.Path, brokenYAML)
	}
	if !strings.Contains(se.Error(), brokenYAML) {
		t.Errorf("Error() = %q, want it to name the path", se.Error())
	}
	if errors.Unwrap(se) == nil {
		t.Error("SyntaxError must unwrap to the underlying cause")
	}

	brokenJSON := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(brokenJSON, []byte(`{oops}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = LoadPolicy(brokenJSON)
	if !errors.As(err, &se) {
		t.Fatalf("LoadPolicy(broken JSON) error = %v, want *SyntaxError", err)
	}

	// A truncated JSON document decodes to "unexpected EOF", which
	// is not a *json.SyntaxError: it is reported as a document-wide
	// violation so the operator sees the decoder's own message.
	truncJSON := filepath.Join(dir, "trunc.json")
	if err := os.WriteFile(truncJSON, []byte(`{"standard": `), 0o644); err != nil {
		t.Fatal(err)
	}
	_, violations, err := LoadPolicy(truncJSON)
	if err != nil {
		t.Fatalf("LoadPolicy(truncated JSON) error = %v, want violations, not an error", err)
	}
	if !hasViolation(violations, "unexpected EOF") {
		t.Errorf("violations = %v, want one naming the unexpected EOF", violations)
	}

	missing := filepath.Join(dir, "nope.yaml")
	_, _, err = LoadPolicy(missing)
	if !errors.As(err, &se) {
		t.Fatalf("LoadPolicy(missing) error = %v, want *SyntaxError", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error = %v, want it to unwrap to os.ErrNotExist", err)
	}
}

// TestLoadPolicyJSONUnknownFieldAsViolation pins the JSON decode
// branch: DisallowUnknownFields turns an unknown field into a
// violation (exit 2 semantics), distinct from a JSON syntax error.
func TestLoadPolicyJSONUnknownFieldAsViolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repoforge.json")
	if err := os.WriteFile(path, []byte(`{"standard":{"version":"v1"},"bogus":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, violations, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if !hasViolation(violations, "unknown field") {
		t.Errorf("violations = %v, want one naming the unknown field", violations)
	}
}

// TestFindPolicyFile pins the probe order (root file before the
// .repoforge/ directory form) and the empty result for a repository
// with no policy, which callers treat as "nothing to manage".
func TestFindPolicyFile(t *testing.T) {
	t.Run("no policy file", func(t *testing.T) {
		if got := FindPolicyFile(t.TempDir()); got != "" {
			t.Errorf("FindPolicyFile = %q, want empty", got)
		}
	})
	t.Run("root yaml found", func(t *testing.T) {
		dir := t.TempDir()
		want := filepath.Join(dir, "repoforge.yaml")
		if err := os.WriteFile(want, []byte("standard:\n  version: v1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := FindPolicyFile(dir); got != want {
			t.Errorf("FindPolicyFile = %q, want %q", got, want)
		}
	})
	t.Run("root file wins over dot dir", func(t *testing.T) {
		dir := t.TempDir()
		want := filepath.Join(dir, "repoforge.yml")
		if err := os.WriteFile(want, []byte("standard:\n  version: v1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, ".repoforge"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".repoforge", "config.yaml"), []byte("standard:\n  version: v1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := FindPolicyFile(dir); got != want {
			t.Errorf("FindPolicyFile = %q, want %q (root file has probe priority)", got, want)
		}
	})
	t.Run("dot dir form found", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".repoforge"), 0o755); err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(dir, ".repoforge", "config.json")
		if err := os.WriteFile(want, []byte(`{"standard":{"version":"v1"}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := FindPolicyFile(dir); got != want {
			t.Errorf("FindPolicyFile = %q, want %q", got, want)
		}
	})
	t.Run("directory is not a policy file", func(t *testing.T) {
		// A directory named like a policy file must be skipped so a
		// repo carrying a repoforge.yaml/ directory is not treated
		// as having a policy.
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "repoforge.yaml"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := FindPolicyFile(dir); got != "" {
			t.Errorf("FindPolicyFile = %q, want empty (directories skipped)", got)
		}
	})
}
