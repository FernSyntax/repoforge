// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/config"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// TestRunLocalOnConformingRepository covers the happy path of the local
// audit: every offline section reports PASS and the loaded policy is
// handed back for the remote half of the audit.
func TestRunLocalOnConformingRepository(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	var rep Report
	root, pol, err := runLocal(&rep, dir)
	if err != nil {
		t.Fatalf("runLocal: %v", err)
	}
	if root == "" {
		t.Error("runLocal returned no repository root")
	}
	if pol == nil {
		t.Fatal("runLocal returned no policy; the remote audit would silently skip every check")
	}
	for _, c := range []struct{ check, want string }{
		{"local/git", "git repository found"},
		{"local/config", "policy repoforge.yaml is valid"},
		{"local/standard-version", "standard version v1 is current"},
		{"local/required-files", "required baseline files present"},
		{"local/ownership", "ownership declarations consistent"},
		{"local/placeholders", "no unresolved template placeholders"},
	} {
		var found bool
		for _, f := range rep.Findings {
			if f.Check == c.check && f.Severity == Pass && strings.Contains(f.Message, c.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing PASS for %s containing %q; got %+v", c.check, c.want, rep.Findings)
		}
	}
	if fails := findingsWithSeverity(rep, Fail); len(fails) > 0 {
		t.Errorf("unexpected FAIL findings: %+v", fails)
	}
}

// TestRunLocalWithoutPolicy covers the no-policy repository: the policy
// finding is a FAIL, and no standard-version PASS may be invented for a
// policy that does not exist.
func TestRunLocalWithoutPolicy(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	write(t, dir, "README.md", "# Fixture\n")
	write(t, dir, "LICENSE", "Apache-2.0\n")
	write(t, dir, "CHANGELOG.md", "# Changelog\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "chore: initial commit")

	var rep Report
	_, pol, err := runLocal(&rep, dir)
	if err != nil {
		t.Fatalf("runLocal: %v", err)
	}
	if pol != nil {
		t.Error("policy = non-nil for a repository with no policy file")
	}
	var configFail bool
	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "local/config" && strings.Contains(f.Message, "policy file missing") {
			configFail = true
		}
	}
	if !configFail {
		t.Errorf("expected local/config FAIL for the missing policy; got %+v", rep.Findings)
	}
	for _, f := range rep.Findings {
		if f.Check == "local/standard-version" {
			t.Errorf("no standard version exists without a policy: %+v", f)
		}
	}
}

// TestRunLocalEmptyStartDir pins the documented default: an omitted
// StartDir means "the current directory", not "no repository".
func TestRunLocalEmptyStartDir(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	t.Chdir(dir)
	var rep Report
	if _, _, err := runLocal(&rep, ""); err != nil {
		t.Fatalf("runLocal with an empty StartDir: %v", err)
	}
	var gitPass bool
	for _, f := range rep.Findings {
		if f.Check == "local/git" && f.Severity == Pass {
			gitPass = true
		}
	}
	if !gitPass {
		t.Errorf("expected a local/git PASS when StartDir defaults to the working directory; got %+v", rep.Findings)
	}
}

// TestRunLocalNotARepository covers the invocation-error path: doctor
// outside a git checkout cannot audit anything, so it must surface an
// error the CLI turns into exit 2 rather than an empty PASS report.
func TestRunLocalNotARepository(t *testing.T) {
	var rep Report
	_, _, err := runLocal(&rep, t.TempDir())
	if err == nil {
		t.Fatal("expected an error outside a git repository")
	}
	if got := ExitCode(rep, err); got != 2 {
		t.Errorf("exit code = %d, want 2", got)
	}
}

// TestRunLocalReportsMissingRequiredFiles maps the validator's
// files/required findings onto local/required-files — one FAIL per
// missing baseline file, never a single lumped message.
func TestRunLocalReportsMissingRequiredFiles(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	for _, name := range []string{"LICENSE", "CHANGELOG.md"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "chore: drop baseline files")

	var rep Report
	if _, _, err := runLocal(&rep, dir); err != nil {
		t.Fatalf("runLocal: %v", err)
	}
	var fails []Finding
	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "local/required-files" {
			fails = append(fails, f)
		}
	}
	if len(fails) != 2 {
		t.Fatalf("local/required-files FAILs = %d, want 2 in %+v", len(fails), rep.Findings)
	}
	for _, f := range fails {
		if !strings.Contains(f.Message, "required baseline file missing") {
			t.Errorf("unexpected message: %q", f.Message)
		}
	}
}

// TestRunLocalReportsUnmatchedOwnershipPattern maps files/ownership
// onto local/ownership: a managed_paths entry that matches nothing is
// the defect that ships an unowned file into the next generated
// repository.
func TestRunLocalReportsUnmatchedOwnershipPattern(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	write(t, dir, "repoforge.yaml", strings.Replace(doctorTestPolicy, "    - LICENSE\n", "    - LICENSE\n    - docs/**/*.md\n", 1))
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "chore: claim an unowned path")

	var rep Report
	if _, _, err := runLocal(&rep, dir); err != nil {
		t.Fatalf("runLocal: %v", err)
	}
	var ownershipFail bool
	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "local/ownership" && strings.Contains(f.Message, "docs/**/*.md") {
			ownershipFail = true
		}
	}
	if !ownershipFail {
		t.Errorf("expected local/ownership FAIL naming the unmatched pattern; got %+v", rep.Findings)
	}
}

// TestRunLocalReportsSchemaViolations maps config/schema onto
// local/config. A policy that loads but violates the schema is drift,
// not a fatal error: the audit must continue so every other section is
// still reported.
func TestRunLocalReportsSchemaViolations(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	write(t, dir, "repoforge.yaml", "standard:\n  version: v1\nrepository:\n  governance_profile: enterprise\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "chore: use an unknown governance profile")

	var rep Report
	if _, _, err := runLocal(&rep, dir); err != nil {
		t.Fatalf("runLocal must not fail on schema violations: %v", err)
	}
	var schemaFail bool
	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "local/config" && strings.Contains(f.Message, "governance_profile") {
			schemaFail = true
		}
	}
	if !schemaFail {
		t.Errorf("expected local/config FAIL for the schema violation; got %+v", rep.Findings)
	}
}

// TestRunLocalReportsPlaceholders maps content/placeholder onto
// local/placeholders: an unresolved template placeholder in a
// generated repository is the defect that ships an ownership
// placeholder to a public README.
func TestRunLocalReportsPlaceholders(t *testing.T) {
	dir := initRepo(t, doctorTestPolicy)
	// Built at runtime so this source file does not trip its own detector.
	write(t, dir, "README.md", "# Fixture\n\nowner: {{ "+" .Owner }}\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "docs: leave a placeholder in the readme")

	var rep Report
	if _, _, err := runLocal(&rep, dir); err != nil {
		t.Fatalf("runLocal: %v", err)
	}
	var placeholderFail bool
	for _, f := range findingsWithSeverity(rep, Fail) {
		if f.Check == "local/placeholders" {
			placeholderFail = true
		}
	}
	if !placeholderFail {
		t.Errorf("expected local/placeholders FAIL; got %+v", rep.Findings)
	}
}

// TestSlugFromOrigin covers origin-URL parsing for every transport the
// GitHub CLI accepts. The slug is what every remote API path is built
// from, so a misparse is an audit against the wrong repository.
func TestSlugFromOrigin(t *testing.T) {
	for _, c := range []struct {
		name string
		url  string
		want string
	}{
		{"ssh", "git@github.com:o/r.git", "o/r"},
		{"ssh without .git", "git@github.com:o/r", "o/r"},
		{"https", "https://github.com/o/r.git", "o/r"},
		{"https without .git", "https://github.com/o/r", "o/r"},
		{"ssh scheme", "ssh://git@github.com/o/r.git", "o/r"},
		{"dotted owner and repo", "git@github.com:some.owner/some.repo.git", "some.owner/some.repo"},
		{"not github", "https://gitlab.com/o/r.git", ""},
		{"local path", "/srv/git/o/r.git", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			git(t, dir, "init", "-b", "main")
			git(t, dir, "remote", "add", "origin", c.url)

			slug, err := SlugFromOrigin(dir)
			if c.want == "" {
				if err == nil {
					t.Fatalf("expected an error for origin %q, got slug %q", c.url, slug)
				}
				if !strings.Contains(err.Error(), "not a recognized GitHub URL") {
					t.Errorf("error = %q, want a not-a-GitHub-URL message", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SlugFromOrigin(%q): %v", c.url, err)
			}
			if slug != c.want {
				t.Errorf("SlugFromOrigin(%q) = %q, want %q", c.url, slug, c.want)
			}
		})
	}
}

// TestSlugFromOriginWithoutRemote covers the repository that has no
// origin: doctor cannot guess which repository to audit, and the
// remediation must be the documented one (pass --repo owner/name).
func TestSlugFromOriginWithoutRemote(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	_, err := SlugFromOrigin(dir)
	if err == nil {
		t.Fatal("expected an error for a repository with no origin remote")
	}
	if !strings.Contains(err.Error(), "--repo owner/name") {
		t.Errorf("error = %q, want the --repo remediation", err)
	}
}

// TestSlugFromOriginOutsideRepository covers a path that is not a
// checkout at all: git fails, and the failure must read as "no
// origin", not as a bare exec error.
func TestSlugFromOriginOutsideRepository(t *testing.T) {
	_, err := SlugFromOrigin(t.TempDir())
	if err == nil {
		t.Fatal("expected an error outside a git repository")
	}
	if !strings.Contains(err.Error(), "no origin remote") {
		t.Errorf("error = %q, want a no-origin message", err)
	}
}

// TestCheckRepositorySettings covers the two repository-level settings
// doctor compares against policy. The visibility default (empty means
// public) is the schema default, so a policy that omits it must still
// be audited rather than waved through.
func TestCheckRepositorySettings(t *testing.T) {
	for _, c := range []struct {
		name       string
		repo       ghub.Repository
		policy     string
		wantVis    Severity
		wantBranch Severity
		visMsg     string
		branchMsg  string
	}{
		{
			name:       "conforming",
			repo:       ghub.Repository{Visibility: "public", DefaultBranch: "main"},
			policy:     doctorTestPolicy,
			wantVis:    Pass,
			wantBranch: Pass,
			visMsg:     "visibility is public",
			branchMsg:  "default branch is main",
		},
		{
			name:       "drifted visibility and branch",
			repo:       ghub.Repository{Visibility: "private", DefaultBranch: "trunk"},
			policy:     doctorTestPolicy,
			wantVis:    Fail,
			wantBranch: Fail,
			visMsg:     `visibility is "private", policy wants "public"`,
			branchMsg:  `default branch is "trunk", policy wants "main"`,
		},
		{
			// An omitted visibility defaults to public in the schema, so
			// a public repository must PASS rather than drift.
			name:       "policy omits visibility",
			repo:       ghub.Repository{Visibility: "public", DefaultBranch: "main"},
			policy:     "standard:\n  version: v1\nownership:\n  managed_paths:\n    - LICENSE\n",
			wantVis:    Pass,
			wantBranch: Pass,
			visMsg:     "visibility is public",
			branchMsg:  "default branch is main",
		},
		{
			name:       "policy omits default branch",
			repo:       ghub.Repository{Visibility: "public", DefaultBranch: "main"},
			policy:     "standard:\n  version: v1\nownership:\n  managed_paths:\n    - LICENSE\n",
			wantVis:    Pass,
			wantBranch: Pass,
			visMsg:     "visibility is public",
			branchMsg:  "default branch is main",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			pol := policyWithText(t, c.policy)
			var rep Report
			checkRepositorySettings(&rep, &c.repo, pol)

			for _, want := range []struct {
				check string
				sev   Severity
				msg   string
			}{
				{"remote/visibility", c.wantVis, c.visMsg},
				{"remote/default-branch", c.wantBranch, c.branchMsg},
			} {
				var found bool
				for _, f := range rep.Findings {
					if f.Check == want.check && f.Severity == want.sev && strings.Contains(f.Message, want.msg) {
						found = true
					}
				}
				if !found {
					t.Errorf("missing %s/%s containing %q; got %+v", want.check, want.sev, want.msg, rep.Findings)
				}
			}
		})
	}
}

// TestBoolFinding covers the one helper every boolean drift check
// shares. The FAIL message must name both sides: a bare "policy wants
// enabled" leaves the reader unable to tell what the repository
// actually has.
func TestBoolFinding(t *testing.T) {
	for _, c := range []struct {
		name    string
		actual  bool
		want    bool
		wantSev Severity
		wantMsg string
	}{
		{"both enabled", true, true, Pass, "squash merge enabled"},
		{"both disabled", false, false, Pass, "squash merge disabled"},
		{"actual enabled, policy disabled", true, false, Fail, "squash merge enabled, policy wants disabled"},
		{"actual disabled, policy enabled", false, true, Fail, "squash merge disabled, policy wants enabled"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var rep Report
			boolFinding(&rep, "remote/merge", "squash merge", c.actual, c.want)
			if len(rep.Findings) != 1 {
				t.Fatalf("findings = %d, want 1 (%+v)", len(rep.Findings), rep.Findings)
			}
			f := rep.Findings[0]
			if f.Severity != c.wantSev {
				t.Errorf("severity = %q, want %q", f.Severity, c.wantSev)
			}
			if f.Message != c.wantMsg {
				t.Errorf("message = %q, want %q", f.Message, c.wantMsg)
			}
			if f.Check != "remote/merge" {
				t.Errorf("check = %q, want remote/merge", f.Check)
			}
		})
	}
}

// TestOnOff pins the two-word vocabulary used in every boolean
// finding; the messages are part of the CLI output contract.
func TestOnOff(t *testing.T) {
	if got := onOff(true); got != "enabled" {
		t.Errorf("onOff(true) = %q, want %q", got, "enabled")
	}
	if got := onOff(false); got != "disabled" {
		t.Errorf("onOff(false) = %q, want %q", got, "disabled")
	}
}

// TestCheckMergeAndFeatureSettings pins the two table-driven boolean
// audits together with the policy accessors they rely on. The
// pointer-valued policy fields default to true when omitted, so a
// policy that says nothing must not read as "everything disabled".
func TestCheckMergeAndFeatureSettings(t *testing.T) {
	conforming := ghub.Repository{
		Visibility:          "public",
		DefaultBranch:       "main",
		AllowSquashMerge:    true,
		AllowMergeCommit:    false,
		AllowRebaseMerge:    false,
		DeleteBranchOnMerge: true,
		AllowAutoMerge:      false,
		HasIssues:           true,
		HasDiscussions:      false,
		HasProjects:         false,
		HasWiki:             true,
	}
	pol := policyWithText(t, doctorTestPolicy)

	var rep Report
	checkMergeSettings(&rep, &conforming, pol)
	checkFeatureSettings(&rep, &conforming, pol)
	if fails := findingsWithSeverity(rep, Fail); len(fails) > 0 {
		t.Fatalf("a conforming repository must not drift: %+v", fails)
	}
	if len(rep.Findings) != 9 {
		t.Errorf("findings = %d, want 9 (5 merge + 4 feature)", len(rep.Findings))
	}

	drifted := conforming
	drifted.AllowMergeCommit = true
	drifted.DeleteBranchOnMerge = false
	drifted.AllowAutoMerge = true
	drifted.HasWiki = false
	var rep2 Report
	checkMergeSettings(&rep2, &drifted, pol)
	checkFeatureSettings(&rep2, &drifted, pol)

	for _, want := range []string{
		"merge commit enabled, policy wants disabled",
		"delete branch on merge disabled, policy wants enabled",
		"auto-merge enabled, policy wants disabled",
		"wiki disabled, policy wants enabled",
	} {
		var found bool
		for _, f := range findingsWithSeverity(rep2, Fail) {
			if f.Message == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing FAIL %q; got %+v", want, rep2.Findings)
		}
	}
}

// TestCheckMergeAndFeatureSettingsDefaults proves the schema defaults
// flow through: a policy that omits merge and github sections wants
// squash + delete-branch + issues enabled, and a repository set up that
// way must PASS.
func TestCheckMergeAndFeatureSettingsDefaults(t *testing.T) {
	pol := policyWithText(t, "standard:\n  version: v1\nownership:\n  managed_paths:\n    - LICENSE\n")
	repo := ghub.Repository{
		Visibility: "public", DefaultBranch: "main",
		AllowSquashMerge: true, DeleteBranchOnMerge: true, HasIssues: true,
	}
	var rep Report
	checkMergeSettings(&rep, &repo, pol)
	checkFeatureSettings(&rep, &repo, pol)
	if fails := findingsWithSeverity(rep, Fail); len(fails) > 0 {
		t.Errorf("schema defaults must not read as drift: %+v", fails)
	}
}

// TestCheckPlanSecurity pins the plan/visibility matrix. The distinction
// it encodes is the one that matters operationally: a feature disabled
// on a public repository is drift a repository owner can fix, while the
// same feature disabled on a private one is a capability gap the owner
// cannot fix — reporting it as FAIL would block merges on a paywall.
func TestCheckPlanSecurity(t *testing.T) {
	for _, c := range []struct {
		name    string
		status  string
		want    bool
		public  bool
		wantSev Severity
		wantMsg string
	}{
		{"enabled and wanted", "enabled", true, true, Pass, "secret scanning enabled"},
		{"enabled but not wanted", "enabled", false, true, Warn, "secret scanning enabled but policy disables it"},
		{"disabled and not wanted", "disabled", false, true, Pass, "secret scanning disabled per policy"},
		{"disabled on public when wanted", "disabled", true, true, Fail, "secret scanning disabled (free for public repositories)"},
		{"disabled on private when wanted", "disabled", true, false, Unsupported, "secret scanning disabled; private repositories require GitHub Secret Protection"},
		{"unknown status on private", "", true, false, Unsupported, "private repositories require GitHub Secret Protection"},
		{"unknown status on public", "", true, true, Fail, "free for public repositories"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var rep Report
			checkPlanSecurity(&rep, "remote/security", "secret scanning", c.status, c.want, c.public, "GitHub Secret Protection")
			if len(rep.Findings) != 1 {
				t.Fatalf("findings = %d, want 1 (%+v)", len(rep.Findings), rep.Findings)
			}
			f := rep.Findings[0]
			if f.Severity != c.wantSev {
				t.Errorf("severity = %q, want %q", f.Severity, c.wantSev)
			}
			if !strings.Contains(f.Message, c.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", f.Message, c.wantMsg)
			}
			if f.Check != "remote/security" {
				t.Errorf("check = %q, want remote/security", f.Check)
			}
		})
	}
}

// policyWithText loads a policy from inline YAML without touching the
// network or the filesystem outside t.TempDir().
func policyWithText(t *testing.T, body string) *config.Policy {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "repoforge.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pol, _, err := config.LoadPolicy(path)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	return pol
}

func policyWithProfile(t *testing.T, profile string) *config.Policy {
	t.Helper()
	return policyWithText(t, "standard:\n  version: v1\nrepository:\n  governance_profile: "+profile+"\nownership:\n  managed_paths:\n    - LICENSE\n")
}
