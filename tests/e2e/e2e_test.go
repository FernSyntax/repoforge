// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FernSyntax/repoforge/configs"
	"github.com/FernSyntax/repoforge/internal/bootstrap"
	"github.com/FernSyntax/repoforge/internal/doctor"
	ghub "github.com/FernSyntax/repoforge/internal/github"
	"github.com/FernSyntax/repoforge/internal/validate"
)

// namePrefix is the mandatory repository name prefix. Created names
// always start with it; anything else is refused before any mutation.
const namePrefix = "repoforge-e2e-"

type e2eConfig struct {
	owner    string
	template string
	token    string
}

func loadConfig(t *testing.T) e2eConfig {
	t.Helper()
	if os.Getenv("REPOFORGE_E2E") != "1" {
		t.Skip("REPOFORGE_E2E != 1: end-to-end tests are explicit opt-in")
	}
	for _, tool := range []string{"git", "gh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("e2e requires %q on PATH: %v", tool, err)
		}
	}
	owner := os.Getenv("REPOFORGE_E2E_OWNER")
	template := os.Getenv("REPOFORGE_E2E_TEMPLATE")
	if owner == "" || template == "" {
		t.Fatal("set REPOFORGE_E2E_OWNER and REPOFORGE_E2E_TEMPLATE (owner/name)")
	}
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		t.Fatal("set GH_TOKEN or GITHUB_TOKEN for the API client")
	}
	return e2eConfig{owner: owner, template: template, token: token}
}

// validTarget reports whether slug is an allowed disposable target:
// owner/name whose name starts with the prefix. This is pure and runs
// in normal CI (no network, no env).
func validTarget(slug string) bool {
	parts := strings.Split(strings.TrimSuffix(slug, ".git"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	return strings.HasPrefix(parts[1], namePrefix)
}

func TestValidTargetGuard(t *testing.T) {
	for _, ok := range []string{"o/repoforge-e2e-123", "o/repoforge-e2e-x"} {
		if !validTarget(ok) {
			t.Errorf("validTarget(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "o/repoforge", "o/production", "o/repoforge-test-app", "o/", "/x", "o/repoforge-e2e", "o/x/repoforge-e2e-1"} {
		if validTarget(bad) {
			t.Errorf("validTarget(%q) = true, want false", bad)
		}
	}
}

// hasScope reports whether an OAuth scope header value contains want.
// The header is comma/space separated; absence means undisclosed
// (fine-grained tokens), never denial.
func hasScope(header, want string) bool {
	for _, f := range strings.FieldsFunc(header, func(r rune) bool { return r == ',' || r == ' ' }) {
		if f == want {
			return true
		}
	}
	return false
}

// waitFor polls probe until true or the timeout elapses. Pure
// polling logic: liveness comes from the caller-supplied probe.
func waitFor(probe func() bool, interval, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if probe() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}

func TestHasScope(t *testing.T) {
	if !hasScope("repo, delete_repo, workflow", "delete_repo") {
		t.Error("comma-separated scopes must match")
	}
	if !hasScope("repo delete_repo", "delete_repo") {
		t.Error("space-separated scopes must match")
	}
	if hasScope("repo, workflow", "delete_repo") {
		t.Error("missing scope must not match")
	}
	if hasScope("", "delete_repo") {
		t.Error("empty header must not match")
	}
}

func TestWaitFor(t *testing.T) {
	if !waitFor(func() bool { return true }, time.Millisecond, time.Second) {
		t.Error("immediately-true probe must succeed")
	}
	calls := 0
	if !waitFor(func() bool { calls++; return calls >= 3 }, time.Millisecond, 5*time.Second) {
		t.Error("eventually-true probe must succeed")
	}
	if waitFor(func() bool { return false }, time.Millisecond, 20*time.Millisecond) {
		t.Error("never-true probe must time out")
	}
}

// run executes a command, failing the test with combined output.
func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// tryRun executes a command, returning output and success instead of
// failing (for expected-failure probes and best-effort cleanup).
func tryRun(dir string, name string, args ...string) (string, bool) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

func gitCommit(t *testing.T, dir string, message string) {
	t.Helper()
	run(t, dir, "git", "add", "-A")
	cmd := exec.Command("git", "-c", "user.name=RepoForge E2E", "-c", "user.email=e2e@example.invalid",
		"commit", "-m", message)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func TestE2ELifecycle(t *testing.T) {
	cfg := loadConfig(t)
	ctx := context.Background()
	checkDeleteScope(t)
	stamp := time.Now().UTC().Format("20060102-150405")
	repo := namePrefix + stamp
	slug := cfg.owner + "/" + repo
	if !validTarget(slug) {
		t.Fatalf("refusing target %q (guard malfunction)", slug)
	}
	t.Logf("target: %s (template %s)", slug, cfg.template)

	// 1. Create the disposable repository from the template.
	run(t, "", "gh", "repo", "create", slug, "--template", cfg.template, "--public")
	t.Cleanup(func() { // 23. Clean up on success and on failure. Best-effort: only
		// this run's disposable, never anything else.
		if out, ok := tryRun("", "gh", "repo", "delete", slug, "--yes"); !ok {
			t.Logf("cleanup: repo delete: %s", out)
		} else {
			t.Logf("cleanup: deleted %s", slug)
		}
	})

	// 2. Clone it (content is guaranteed present by waitForContent).
	waitForContent(t, slug)
	workParent := t.TempDir()
	run(t, workParent, "gh", "repo", "clone", slug, "work")
	dir := filepath.Join(workParent, "work")

	// 3. Inspect the template baseline: one unrelated commit, no
	// engine history, no implementation source.
	log := run(t, dir, "git", "log", "--oneline")
	if lines := strings.Count(strings.TrimSpace(log), "\n") + 1; lines != 1 {
		t.Fatalf("expected a single baseline commit, got %d:\n%s", lines, log)
	}
	if matches, _ := tryRun(dir, "find", ".", "-path", "./.git", "-prune", "-o", "-type", "f",
		"(", "-name", "*.go", "-o", "-name", "go.mod", "-o", "-name", "go.sum", ")", "-print"); matches != "" {
		t.Fatalf("engine source leaked into template output:\n%s", matches)
	}

	// 4. Validate: placeholders are unresolved by design, so the gate
	// must report them (not pass silently).
	vrep, verr := validate.Run(validate.Options{StartDir: dir})
	if verr != nil {
		t.Fatalf("validate: %v", verr)
	}
	if !hasRule(vrep, "content/placeholder") {
		t.Fatalf("fresh template must report placeholders, got: %+v", vrep.Findings)
	}
	// Minimal adoption for tooling: the schema version is a constant,
	// identical for every consumer — everything else stays generic.
	resolveVersion(t, dir)

	client := ghub.New(ghub.WithToken(cfg.token))
	bootOpts := func(dry bool) bootstrap.Options {
		return bootstrap.Options{StartDir: dir, Repo: slug, DryRun: dry, Yes: true, Client: client}
	}

	// 5. Dry-run plans work and changes nothing observable.
	dry, err := bootstrap.Run(ctx, bootOpts(true))
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !hasApplicable(dry.Plan) {
		t.Fatal("dry-run on a fresh repository should plan changes")
	}

	// 6. Bootstrap converges the repository.
	first, err := bootstrap.Run(ctx, bootOpts(false))
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !first.Converged {
		t.Fatalf("did not converge: %+v", first.Plan)
	}

	// 7. Doctor is remotely healthy.
	assertRemoteHealthy(t, dir, client, slug)

	// 8. Required rulesets exist.
	rulesets, err := client.ListRulesets(ctx, cfg.owner, repo)
	if err != nil {
		t.Fatalf("list rulesets: %v", err)
	}
	for _, want := range []string{configs.MainRulesetName, configs.ReleaseTagsRulesetName} {
		var found bool
		for _, rs := range rulesets {
			if rs.Name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("owned ruleset %q missing in %+v", want, rulesets)
		}
	}

	// 9. Required labels exist.
	labels, err := client.ListLabels(ctx, cfg.owner, repo)
	if err != nil {
		t.Fatalf("list labels: %v", err)
	}
	wantLabels, err := configs.CanonicalLabels()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, l := range labels {
		have[l.Name] = true
	}
	for _, w := range wantLabels {
		if !have[w.Name] {
			t.Fatalf("canonical label %q missing", w.Name)
		}
	}

	// 10. Repository settings match the standard defaults.
	got, err := client.GetRepository(ctx, cfg.owner, repo)
	if err != nil {
		t.Fatalf("get repository: %v", err)
	}
	check := func(name string, actual, want bool) {
		t.Helper()
		if actual != want {
			t.Errorf("%s = %t, want %t", name, actual, want)
		}
	}
	check("allow_squash_merge", got.AllowSquashMerge, true)
	check("allow_merge_commit", got.AllowMergeCommit, false)
	check("allow_rebase_merge", got.AllowRebaseMerge, false)
	check("delete_branch_on_merge", got.DeleteBranchOnMerge, true)
	if got.Visibility != "public" || got.DefaultBranch != "main" {
		t.Errorf("visibility/branch = %s/%s", got.Visibility, got.DefaultBranch)
	}

	// 11. Modify one remote setting out of band.
	runStdin(t, "repos/"+slug, `{"allow_merge_commit": true}`)

	// 12. Doctor detects the drift.
	drep := doctorRun(t, dir, client, slug)
	assertFailContains(t, drep, "remote/merge", "merge commit")

	// 13. Bootstrap reconciles.
	rec, err := bootstrap.Run(ctx, bootOpts(false))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !rec.Converged {
		t.Fatalf("did not reconverge: %+v", rec.Plan)
	}

	// 14. Doctor is healthy again.
	assertRemoteHealthy(t, dir, client, slug)

	// 15. Branch following the standard (via a real issue number).
	issueURL := run(t, "", "gh", "issue", "create", "--repo", slug,
		"--title", "E2E probe change", "--body", "Disposable verification change.")
	issueNum := lastPathSegment(t, issueURL)
	branch := "chore/" + issueNum + "-e2e-probe"
	run(t, dir, "git", "checkout", "-b", branch)

	// 16-17. Trivial change as a Conventional Commit.
	if err := os.WriteFile(filepath.Join(dir, "e2e-probe.txt"), []byte("probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "chore(e2e): probe change")

	// 18. Push.
	run(t, dir, "git", "push", "-u", "origin", branch)

	// 19. Open a PR.
	prURL := run(t, dir, "gh", "pr", "create", "--title", "chore(e2e): probe change",
		"--body", "Disposable verification change. Closes #"+issueNum+".")
	prNum := lastPathSegment(t, prURL)

	// 20. Verify CI (poll to green, fail on red).
	waitForChecks(t, slug, prNum)

	// 21. Merge the PR (squash, delete branch).
	run(t, dir, "gh", "pr", "merge", prNum, "--squash", "--delete-branch")

	// 22. Confirm branch deletion.
	if out, ok := tryRun("", "gh", "api", "repos/"+slug+"/branches/"+branch); ok {
		t.Fatalf("branch still exists after merge: %s", out)
	}
	t.Log("lifecycle complete: create → govern → drift → reconcile → PR → merge → cleanup pending")
}

// resolveVersion sets the schema-constant version so tooling can load
// the policy. All other placeholders stay generic by design.
func resolveVersion(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, ".repoforge", "config.yml")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Built at runtime so this source file does not trip the
	// repository's own placeholder detector.
	ph := "{{" + " .StandardVersion }}"
	updated := strings.Replace(string(data), `version: "`+ph+`"`, `version: "v1"`, 1)
	if updated == string(data) {
		t.Fatal("config.yml has no StandardVersion placeholder to resolve")
	}
	if err := os.WriteFile(p, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasRule(rep validate.Report, rule string) bool {
	for _, f := range rep.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func hasApplicable(plan []bootstrap.PlanItem) bool {
	for _, item := range plan {
		if item.Action == bootstrap.ActionCreate || item.Action == bootstrap.ActionUpdate {
			return true
		}
	}
	return false
}

func doctorRun(t *testing.T, dir string, client *ghub.Client, slug string) doctor.Report {
	t.Helper()
	rep, err := doctor.Run(context.Background(), doctor.Options{StartDir: dir, Repo: slug, Client: client})
	if err != nil {
		t.Fatalf("doctor.Run: %v", err)
	}
	return rep
}

// assertRemoteHealthy fails on any remote finding: local placeholder
// findings are the designed state of an unadopted template copy.
func assertRemoteHealthy(t *testing.T, dir string, client *ghub.Client, slug string) {
	t.Helper()
	rep := doctorRun(t, dir, client, slug)
	for _, f := range rep.Findings {
		if f.Severity == doctor.Fail && strings.HasPrefix(string(f.Check), "remote/") {
			t.Fatalf("remote drift after converge: %+v", rep.Findings)
		}
	}
}

func assertFailContains(t *testing.T, rep doctor.Report, check, snippet string) {
	t.Helper()
	for _, f := range rep.Findings {
		if f.Severity == doctor.Fail && string(f.Check) == check && strings.Contains(f.Message, snippet) {
			return
		}
	}
	t.Fatalf("no FAIL %s containing %q in %+v", check, snippet, rep.Findings)
}

// checkDeleteScope fails fast when the token provably lacks repo
// deletion: cleanup could not run, so creating anything would orphan
// it. Undisclosed scopes (fine-grained tokens) proceed — deletion is
// attempted best-effort at cleanup.
func checkDeleteScope(t *testing.T) {
	t.Helper()
	out, ok := tryRun("", "gh", "api", "-i", "rate_limit")
	if !ok {
		t.Fatalf("cannot reach the API to check delete scope: %s", out)
	}
	var scopes string
	for _, line := range strings.Split(out, "\n") {
		if idx := strings.Index(line, ":"); idx > 0 {
			if strings.EqualFold(strings.TrimSpace(line[:idx]), "x-oauth-scopes") {
				scopes = strings.TrimSpace(line[idx+1:])
			}
		}
	}
	if scopes == "" {
		t.Log("token scopes undisclosed; cleanup will be attempted best-effort")
		return
	}
	if !hasScope(scopes, "delete_repo") {
		t.Fatalf("token lacks delete_repo scope (%q): cleanup could not run, refusing to create anything (fix: gh auth refresh -s delete_repo)", scopes)
	}
}

// waitForContent polls until the template-generated initial commit
// exists. Creation returns before GitHub generates it; cloning
// immediately yields an empty repository.
func waitForContent(t *testing.T, slug string) {
	t.Helper()
	branch, ok := tryRun("", "gh", "api", "repos/"+slug, "--jq", ".default_branch")
	if !ok {
		t.Fatalf("cannot read default branch of %s: %s", slug, branch)
	}
	probe := func() bool {
		_, ok := tryRun("", "gh", "api", "repos/"+slug+"/commits", "-f", "sha="+branch, "-f", "per_page=1")
		return ok
	}
	if !waitFor(probe, 10*time.Second, 5*time.Minute) {
		t.Fatalf("template content for %s did not appear within 5 minutes", slug)
	}
}

// lastPathSegment returns the final URL segment, asserting it is a
// positive issue/PR number.
func lastPathSegment(t *testing.T, url string) string {
	t.Helper()
	seg := url[strings.LastIndex(url, "/")+1:]
	for _, r := range seg {
		if r < '0' || r > '9' {
			t.Fatalf("not a numeric reference %q in %q", seg, url)
		}
	}
	if seg == "" || seg == "0" {
		t.Fatalf("not a numeric reference %q in %q", seg, url)
	}
	return seg
}
func runStdin(t *testing.T, path, body string) {
	t.Helper()
	cmd := exec.Command("gh", "api", "--method", "PATCH", path, "--input", "-")
	cmd.Stdin = strings.NewReader(body)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gh api PATCH %s: %v\n%s", path, err, out)
	}
}

// waitForChecks polls the PR's checks until all complete: any failure
// fails the test, timeout fails the test.
func waitForChecks(t *testing.T, slug, pr string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Minute)
	for {
		out, ok := tryRun("", "gh", "pr", "view", pr, "--repo", slug,
			"--json", "statusCheckRollup", "--jq",
			`[.statusCheckRollup[] | "\(.status):\(.conclusion // "-"):\(.name)"] | join(",")`)
		if !ok {
			t.Fatalf("reading PR checks: %s", out)
		}
		pending, failed := false, false
		for _, part := range strings.Split(out, ",") {
			switch {
			case strings.HasPrefix(part, "COMPLETED:FAILURE"), strings.HasPrefix(part, "COMPLETED:CANCELLED"),
				strings.HasPrefix(part, "COMPLETED:TIMED_OUT"), strings.HasPrefix(part, "COMPLETED:ACTION_REQUIRED"):
				failed = true
			case !strings.HasPrefix(part, "COMPLETED:"):
				pending = true
			}
		}
		t.Logf("checks: %s", out)
		if failed {
			t.Fatalf("PR checks failed: %s", out)
		}
		if !pending {
			if out == "" {
				t.Fatal("no checks reported on the PR")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for PR checks: %s", out)
		}
		time.Sleep(30 * time.Second)
	}
}
