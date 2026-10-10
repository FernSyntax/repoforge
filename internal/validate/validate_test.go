package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/brand"
)

const validPolicy = `standard:
  name: repoforge
  version: v1
repository:
  governance_profile: solo
github:
  issues: true
  wiki: true
ownership:
  managed_paths:
    - LICENSE
`

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test Author",
		"GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=Test Author",
		"GIT_COMMITTER_EMAIL=test@example.invalid",
		"LC_ALL=C",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", message)
}

// initRepo creates a fully valid managed repository fixture.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	write(t, dir, "README.md", "# Fixture\n")
	write(t, dir, "LICENSE", "Apache-2.0\n")
	write(t, dir, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n")
	write(t, dir, "repoforge.yaml", validPolicy)
	commitAll(t, dir, "chore: initial commit")
	return dir
}

func run(t *testing.T, dir string) (Report, error) {
	t.Helper()
	return Run(Options{StartDir: dir, MaxCommits: 20})
}

func hasFinding(rep Report, rule string) bool {
	for _, f := range rep.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

// hasRule is hasFinding under the name the finding rule uses.
func hasRule(rep Report, rule string) bool { return hasFinding(rep, rule) }

func TestValidRepository(t *testing.T) {
	dir := initRepo(t)
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rep.Valid() {
		t.Fatalf("expected valid repository, got findings: %+v", rep.Findings)
	}
	if got := ExitCode(rep, err); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
}

func TestMissingRequiredFile(t *testing.T) {
	dir := initRepo(t)
	if err := os.Remove(filepath.Join(dir, "LICENSE")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "chore: remove license")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFinding(rep, "files/required") {
		t.Fatalf("expected files/required finding, got: %+v", rep.Findings)
	}
	if got := ExitCode(rep, err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
}

func TestMalformedYAML(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "repoforge.yaml", "standard: [not, closed\n")
	rep, err := run(t, dir)
	if err == nil {
		t.Fatalf("expected configuration error, got report: %+v", rep.Findings)
	}
	if got := ExitCode(rep, err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

func TestUnresolvedPlaceholder(t *testing.T) {
	dir := initRepo(t)
	// Built at runtime so this source file does not trip its own detector.
	write(t, dir, "notes.txt", "owner is {{ "+" .Owner }}\n")
	commitAll(t, dir, "docs: add notes")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFinding(rep, "content/placeholder") {
		t.Fatalf("expected content/placeholder finding, got: %+v", rep.Findings)
	}
}

func TestObviousSecret(t *testing.T) {
	dir := initRepo(t)
	// Built at runtime so this source file does not trip its own detector.
	write(t, dir, "token.txt", "ghp_"+"0123456789abcdef0123456789abcdef0123\n")
	commitAll(t, dir, "chore: add token file")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFinding(rep, "content/secret") {
		t.Fatalf("expected content/secret finding, got: %+v", rep.Findings)
	}
}

func TestEmptyIssueFormTitle(t *testing.T) {
	dir := initRepo(t)
	// GitHub rejects an empty title key; the key must be omitted.
	write(t, dir, ".github/ISSUE_TEMPLATE/task.yml", "name: Task\ndescription: Scoped work\ntitle: \"\"\nbody:\n  - type: textarea\n    id: scope\n    attributes:\n      label: Scope\n")
	commitAll(t, dir, "chore: add form")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFinding(rep, "yaml/issue-template") {
		t.Fatalf("expected yaml/issue-template finding for empty title, got: %+v", rep.Findings)
	}
}

func TestInvalidCommitMessage(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "extra.txt", "x\n")
	commitAll(t, dir, "this is not a conventional commit")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFinding(rep, "commit/message") {
		t.Fatalf("expected commit/message finding, got: %+v", rep.Findings)
	}
}

func TestInvalidBranchName(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "checkout", "-b", "feature")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFinding(rep, "branch/name") {
		t.Fatalf("expected branch/name finding, got: %+v", rep.Findings)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "repoforge.yaml", "standard:\n  version: v1\nrepository:\n  governance_profile: enterprise\n")
	commitAll(t, dir, "chore: break policy")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFinding(rep, "config/schema") {
		t.Fatalf("expected config/schema finding, got: %+v", rep.Findings)
	}
	if got := ExitCode(rep, err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
}

// TestLegacyBrandingIsRejected is the regression test for the branding
// leak gate: a legacy product reference in a tracked file must fail
// validation, and the current product name must not.
func TestLegacyBrandingIsRejected(t *testing.T) {
	dir := initRepo(t)
	legacy := brand.Normalize("Repo", "Doctrine")
	write(t, dir, "docs/notes.md", "the standard used to be "+legacy+"\n")
	commitAll(t, dir, "docs: add notes")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasRule(rep, "content/branding") {
		t.Fatalf("expected content/branding finding, got: %+v", rep.Findings)
	}
	if got := ExitCode(rep, err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
}

// TestCurrentBrandingIsAccepted proves the gate is a leak gate, not a
// blanket word filter: the RepoForge name in every spelling passes.
func TestCurrentBrandingIsAccepted(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "docs/notes.md", "RepoForge, repoforge, repo-forge, REPOFORGE\n")
	commitAll(t, dir, "docs: add notes")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(rep, "content/branding") {
		t.Fatalf("the current product name must not trip the branding gate: %+v", rep.Findings)
	}
}

func TestNotAGitRepository(t *testing.T) {
	dir := t.TempDir()
	_, err := run(t, dir)
	if err == nil {
		t.Fatal("expected error outside a git repository")
	}
	if got := ExitCode(Report{}, err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

// TestADRsAreIndexedAndResolvable is the structural half of the ADR
// process (spec.md §20.5): every record under docs/decisions/ is listed
// in the index, and every indexed name resolves to a real file.
func TestADRsAreIndexedAndResolvable(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "docs/decisions/README.md", "# Decisions\n\n| ADR | Status |\n| --- | --- |\n| 0001 | [one](0001-first.md) | accepted |\n")
	write(t, dir, "docs/decisions/0001-first.md", "# 0001\n")
	commitAll(t, dir, "docs: add an ADR")

	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(rep, "decisions/unindexed") || hasRule(rep, "decisions/broken-reference") {
		t.Fatalf("a fully indexed ADR set must pass: %+v", rep.Findings)
	}

	// An unindexed record is a finding.
	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "0002-second.md"), []byte("# 0002\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "docs: add a second ADR")
	rep, err = run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasRule(rep, "decisions/unindexed") {
		t.Fatalf("an unindexed ADR must be reported: %+v", rep.Findings)
	}

	// An index row with no file is a finding.
	write(t, dir, "docs/decisions/README.md", "# Decisions\n\n| ADR | Status |\n| --- | --- |\n| 0009 | [missing](0009-nowhere.md) | accepted |\n")
	commitAll(t, dir, "docs: index a record that does not exist")
	rep, err = run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasRule(rep, "decisions/broken-reference") {
		t.Fatalf("a dangling index row must be reported: %+v", rep.Findings)
	}
}

// TestRepositoryWithoutDecisionsIsAccepted proves the check is scoped to
// repositories that actually keep ADRs.
func TestRepositoryWithoutDecisionsIsAccepted(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	write(t, dir, "README.md", "# Fixture\n")
	commitAll(t, dir, "chore: initial commit")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(rep, "decisions/unindexed") || hasRule(rep, "decisions/broken-reference") {
		t.Fatalf("a repository without ADRs must not be penalised: %+v", rep.Findings)
	}
}

// dottedPlaceholder builds a real RepoForge placeholder without writing
// it literally, so this test file is not itself a placeholder leak.
func dottedPlaceholder() string { return "{{ " + ".ProjectName" + " }}" }

// TestGitHubExpressionsAreNotPlaceholders pins the placeholder syntax:
// GitHub's ${{ }} expressions must not read as unresolved RepoForge
// placeholders, which is what keeps every workflow out of
// ownership.placeholder_paths.
func TestGitHubExpressionsAreNotPlaceholders(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".github/workflows/demo.yml",
		"name: Demo\non: [push]\njobs:\n  demo:\n    runs-on: ubuntu-latest\n"+
			"    steps:\n      - run: echo \"${{ github.ref }} ${{ github.workflow }}\"\n")
	commitAll(t, dir, "ci: add a workflow that uses expressions")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(rep, "content/placeholder") {
		t.Fatalf("a GitHub expression must not read as a RepoForge placeholder: %+v", rep.Findings)
	}
}

// TestDottedPlaceholdersAreStillRejected proves the tightening did not
// disable the check: a real RepoForge placeholder is still a violation.
func TestDottedPlaceholdersAreStillRejected(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "notes.md", "subscription: "+dottedPlaceholder()+"\n")
	commitAll(t, dir, "docs: add notes")
	rep, err := run(t, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasRule(rep, "content/placeholder") {
		t.Fatalf("a dotted placeholder must still be reported: %+v", rep.Findings)
	}
}
