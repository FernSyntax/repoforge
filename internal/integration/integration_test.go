package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/bootstrap"
	"github.com/FernSyntax/repoforge/internal/doctor"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// testRepo resolves the target repository or skips. The slug guard
// keeps real product repositories out: only obvious throwaways run.
func testRepo(t *testing.T) (owner, repo, token string) {
	t.Helper()
	slug := os.Getenv("REPOFORGE_TEST_REPO")
	if slug == "" {
		t.Skip("REPOFORGE_TEST_REPO not set; integration tests are opt-in")
	}
	parts := strings.Split(strings.TrimSuffix(slug, ".git"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Fatalf("REPOFORGE_TEST_REPO=%q is not an owner/name slug", slug)
	}
	if !strings.Contains(strings.ToLower(slug), "test") {
		t.Fatalf("refusing slug %q: integration tests only run against repositories that look like throwaways (slug must contain %q)", slug, "test")
	}
	token = os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		t.Skip("no token: set GH_TOKEN or GITHUB_TOKEN to run integration tests")
	}
	return parts[0], parts[1], token
}

func testClient(t *testing.T, token string) *ghub.Client {
	t.Helper()
	return ghub.New(ghub.WithToken(token))
}

// initFixtureRepo builds a minimal valid local repository carrying a
// policy. For apply runs the policy mirrors the target's visibility
// and default branch (read first) so bootstrap never flips those.
func initFixtureRepo(t *testing.T, policy string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=Test Author", "GIT_COMMITTER_EMAIL=test@example.invalid",
			"LC_ALL=C",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(name, content string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	write("README.md", "# Fixture\n")
	write("LICENSE", "Apache-2.0\n")
	write("CHANGELOG.md", "# Changelog\n")
	write("repoforge.yaml", policy)
	run("add", "-A")
	run("commit", "-m", "chore: initial commit")
	return dir
}

const fixturePolicy = `standard:
  version: v1
repository:
  governance_profile: solo
  visibility: public
  default_branch: main
github:
  issues: true
  wiki: true
ownership:
  managed_paths:
    - LICENSE
`

func TestDoctorAgainstTestRepo(t *testing.T) {
	owner, repo, token := testRepo(t)
	dir := initFixtureRepo(t, fixturePolicy)
	rep, err := doctor.Run(context.Background(), doctor.Options{
		StartDir: dir, Repo: owner + "/" + repo, Client: testClient(t, token),
	})
	if err != nil {
		t.Fatalf("doctor.Run: %v", err)
	}
	if rep.Repository != owner+"/"+repo {
		t.Fatalf("report targets %q, want %q/%s", rep.Repository, owner, repo)
	}
	pass, warn, fail, unsupported := rep.Summary()
	t.Logf("doctor summary: pass=%d warn=%d fail=%d unsupported=%d", pass, warn, fail, unsupported)
}

func TestBootstrapDryRunAgainstTestRepo(t *testing.T) {
	owner, repo, token := testRepo(t)
	dir := initFixtureRepo(t, fixturePolicy)
	res, err := bootstrap.Run(context.Background(), bootstrap.Options{
		StartDir: dir, Repo: owner + "/" + repo, DryRun: true, Client: testClient(t, token),
	})
	if err != nil {
		t.Fatalf("bootstrap dry-run: %v", err)
	}
	if !res.DryRun {
		t.Error("result does not record the dry run")
	}
	var applicable int
	for _, item := range res.Plan {
		if item.Action == bootstrap.ActionCreate || item.Action == bootstrap.ActionUpdate {
			applicable++
		}
	}
	t.Logf("dry-run plan: %d item(s), %d applicable", len(res.Plan), applicable)
}

func TestBootstrapApplyAgainstTestRepo(t *testing.T) {
	owner, repo, token := testRepo(t)
	if os.Getenv("REPOFORGE_TEST_APPLY") != "1" {
		t.Skip("REPOFORGE_TEST_APPLY != 1: converging would mutate the test repository")
	}
	ctx := context.Background()
	client := testClient(t, token)

	// Mirror the target's visibility and default branch so the only
	// changes bootstrap makes are the managed security posture,
	// rulesets, and labels — never visibility or renames.
	actual, err := client.GetRepository(ctx, owner, repo)
	if err != nil {
		t.Fatalf("reading test repository: %v", err)
	}
	if actual.Visibility != "public" {
		t.Fatalf("refusing apply on non-public test repository %s/%s (plan-gated features would not converge)", owner, repo)
	}
	policy := strings.Replace(fixturePolicy, "default_branch: main",
		fmt.Sprintf("default_branch: %s", actual.DefaultBranch), 1)
	dir := initFixtureRepo(t, policy)
	opts := bootstrap.Options{StartDir: dir, Repo: owner + "/" + repo, Yes: true, Client: client}

	first, err := bootstrap.Run(ctx, opts)
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if !first.Converged {
		t.Fatalf("test repository did not converge: %+v", first.Plan)
	}
	second, err := bootstrap.Run(ctx, opts)
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if second.Applied != 0 || !second.Converged {
		t.Fatalf("not idempotent on test repository: applied=%d converged=%t plan=%+v",
			second.Applied, second.Converged, second.Plan)
	}
}
