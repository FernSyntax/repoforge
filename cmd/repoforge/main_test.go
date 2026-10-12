package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is the engine checkout, used to exercise the real template
// source default.
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func adoptionArgs() []string {
	return []string{
		"template", "adopt",
		"--var", "ProjectName=demo",
		"--var", "Owner=acme",
		"--var", "RepoURL=https://github.com/acme/demo",
		"--var", "Description=A demo project",
		"--var", "StandardVersion=v1",
	}
}

// TestTemplateAdoptDefaultsToWorkingDirectory pins the documented default
// contract: from inside a generated repository, the advertised
// `repoforge template adopt ...` works with no --path at all.
func TestTemplateAdoptDefaultsToWorkingDirectory(t *testing.T) {
	dir := generatedRepo(t)
	chdir(t, dir)

	var out, errOut bytes.Buffer
	code := run(append(adoptionArgs(), "--dry-run"), &out, &errOut)
	if code != 1 {
		t.Fatalf("dry-run exit = %d, want 1 (changes pending)\nstdout: %s\nstderr: %s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "would substitute") {
		t.Errorf("dry-run did not print planned substitutions:\n%s", out.String())
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "README.md")); strings.Contains(string(data), "# demo") {
		t.Error("dry-run modified the repository")
	}

	out.Reset()
	errOut.Reset()
	code = run(adoptionArgs(), &out, &errOut)
	if code != 0 {
		t.Fatalf("adopt exit = %d, want 0\nstdout: %s\nstderr: %s", code, out.String(), errOut.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# demo") {
		t.Errorf("README not adopted:\n%s", data)
	}
	if strings.Contains(string(data), "{{"+" .ProjectName }}") {
		t.Errorf("placeholder survived adoption:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, ".repoforge", "config.yml")); err != nil {
		t.Fatal(err)
	}
	policy, _ := os.ReadFile(filepath.Join(dir, ".repoforge", "config.yml"))
	if !strings.Contains(string(policy), `version: "v1"`) {
		t.Errorf("policy not adopted:\n%s", policy)
	}

	// Idempotent: a second run is a no-op with exit 0.
	out.Reset()
	errOut.Reset()
	if code := run(adoptionArgs(), &out, &errOut); code != 0 {
		t.Fatalf("second adopt exit = %d, want 0\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "already adopted") {
		t.Errorf("second run did not report being a no-op:\n%s", out.String())
	}
}

// TestTemplateAdoptHonoursExplicitPath proves --path is honoured
// verbatim, in both spellings: the previous implementation rewrote the
// string "template" to "." and silently ignored an explicit value.
func TestTemplateAdoptHonoursExplicitPath(t *testing.T) {
	dir := generatedRepo(t)

	var out, errOut bytes.Buffer
	args := append([]string{"template", "adopt", "--path", dir}, adoptionArgs()[2:]...)
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("explicit --path exit = %d, want 0\n%s%s", code, out.String(), errOut.String())
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "README.md")); !strings.Contains(string(data), "# demo") {
		t.Errorf("explicit --path was not honoured:\n%s", data)
	}

	// A trailing slash must be honoured too.
	dir2 := generatedRepo(t)
	out.Reset()
	errOut.Reset()
	args = append([]string{"template", "adopt", "--path", dir2 + "/"}, adoptionArgs()[2:]...)
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("--path with trailing slash exit = %d, want 0\n%s%s", code, out.String(), errOut.String())
	}
	if data, _ := os.ReadFile(filepath.Join(dir2, "README.md")); !strings.Contains(string(data), "# demo") {
		t.Errorf("--path with trailing slash was not honoured:\n%s", data)
	}
}

// TestTemplateValidateDefaultsToCanonicalSource pins the other half of
// the per-command default contract: the template-source default is only
// correct for the template-source commands.
func TestTemplateValidateDefaultsToCanonicalSource(t *testing.T) {
	chdir(t, repoRoot(t))

	var out, errOut bytes.Buffer
	if code := run([]string{"template", "validate"}, &out, &errOut); code != 0 {
		t.Fatalf("template validate exit = %d, want 0\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "template validate: OK") {
		t.Errorf("unexpected output:\n%s", out.String())
	}

	// Running it with no template/ present must fail loudly, not
	// silently adopt the working directory.
	empty := t.TempDir()
	chdir(t, empty)
	out.Reset()
	errOut.Reset()
	if code := run([]string{"template", "validate"}, &out, &errOut); code == 0 {
		t.Errorf("template validate in a directory with no template/ must fail\n%s%s", out.String(), errOut.String())
	}
}

// TestTemplateBuildDefaultsToCanonicalSourceAndRefusesDangerousOutput
// exercises the CLI-level output-path policy.
func TestTemplateBuildDefaultsToCanonicalSourceAndRefusesDangerousOutput(t *testing.T) {
	root := repoRoot(t)
	chdir(t, root)

	// A dedicated output directory inside the project is allowed.
	out := filepath.Join(t.TempDir(), "out")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"template", "build", "--out", out,
		"--var", "ProjectName=demo", "--var", "Owner=acme",
		"--var", "RepoURL=https://github.com/acme/demo",
		"--var", "Description=A demo project", "--var", "StandardVersion=v1"}, &stdout, &stderr); code != 0 {
		t.Fatalf("build exit = %d, want 0\n%s%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Errorf("build produced no output: %v", err)
	}

	// Dangerous targets are refused through the CLI as well.
	for _, target := range []string{".", "./", "..", "/", "template"} {
		stdout.Reset()
		stderr.Reset()
		code := run([]string{"template", "build", "--out", target}, &stdout, &stderr)
		if code == 0 {
			t.Errorf("--out %q must be refused", target)
		}
		if !strings.Contains(stderr.String(), "refusing") {
			t.Errorf("--out %q: refusal message missing: %s", target, stderr.String())
		}
	}
	// The template source must be untouched by the refusals.
	if _, err := os.Stat(filepath.Join(root, "template", "README.md")); err != nil {
		t.Errorf("template source was damaged: %v", err)
	}
}

// TestTemplateAdoptRejectsNonGeneratedRepository proves the adoption
// guard is enforced at the CLI.
func TestTemplateAdoptRejectsNonGeneratedRepository(t *testing.T) {
	plain := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run(append([]string{"template", "adopt", "--path", plain}, adoptionArgs()[2:]...), &out, &errOut); code != 2 {
		t.Fatalf("exit = %d, want 2 for a non-generated repository\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), ".repoforge/config.yml") {
		t.Errorf("refusal does not name the missing generated policy: %s", errOut.String())
	}
}

// TestTemplateAdoptInvalidVarIsAUsageError pins the exit-code contract
// for malformed input.
func TestTemplateAdoptInvalidVarIsAUsageError(t *testing.T) {
	dir := generatedRepo(t)
	var out, errOut bytes.Buffer
	args := []string{"template", "adopt", "--path", dir, "--var", "ProjectName=demo"}
	if code := run(args, &out, &errOut); code != 2 {
		t.Fatalf("exit = %d, want 2\n%s%s", code, out.String(), errOut.String())
	}
	args = []string{"template", "adopt", "--path", dir, "--var", "nonsense"}
	if code := run(args, &out, &errOut); code != 2 {
		t.Fatalf("malformed --var exit = %d, want 2", code)
	}
}

// TestTemplateAdoptThenValidateProvesGeneratedRepoSatisfiesPolicy runs
// the repository's own local verification flow on an adopted generated
// repository.
func TestTemplateAdoptThenValidateProvesGeneratedRepoSatisfiesPolicy(t *testing.T) {
	dir := generatedRepo(t)
	if err := gitInit(t, dir); err != nil {
		t.Skipf("git unavailable: %v", err)
	}

	var out, errOut bytes.Buffer
	chdir(t, dir)
	if code := run(adoptionArgs(), &out, &errOut); code != 0 {
		t.Fatalf("adopt exit = %d\n%s%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	code := run([]string{"validate"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("validate exit = %d, want 0\n%s%s", code, out.String(), errOut.String())
	}
	// Before adoption the same repository must fail validation on the
	// placeholder rule — that is the deliberate template contract.
}

// TestTemplateBuildRejectsNonEmptyWithoutForce pins the CLI overwrite
// contract and the --force escape hatch.
func TestTemplateBuildRejectsNonEmptyWithoutForce(t *testing.T) {
	chdir(t, repoRoot(t))
	out := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "only-copy.txt"), []byte("irreplaceable"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"template", "build", "--out", out}
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code == 0 {
		t.Fatal("non-empty destination must be refused")
	}
	if _, err := os.Stat(filepath.Join(out, "only-copy.txt")); err != nil {
		t.Errorf("refused build destroyed the destination: %v", err)
	}
	args = append(args, "--force",
		"--var", "ProjectName=demo", "--var", "Owner=acme",
		"--var", "RepoURL=https://github.com/acme/demo",
		"--var", "Description=A demo project", "--var", "StandardVersion=v1")
	stdout.Reset()
	stderr.Reset()
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("forced build exit = %d, want 0\n%s%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Errorf("forced build produced no output: %v", err)
	}
}

// generatedRepo builds a generated Go repository the way the template
// pipeline does: the model baseline plus a real Go project.
func generatedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ph := func(name string) string { return "{{" + " ." + name + " }}" }
	files := map[string]string{
		"README.md":                        "# " + ph("ProjectName") + "\n\n" + ph("Description") + "\n",
		"LICENSE":                          "Copyright " + ph("Owner") + "\n",
		"CHANGELOG.md":                     "# Changelog\n",
		"CODE_OF_CONDUCT.md":               "# CoC\n",
		"CONTRIBUTING.md":                  "# Contributing\n",
		"SECURITY.md":                      "# Security\n",
		"SUPPORT.md":                       "# Support\n",
		"AGENTS.md":                        "# Agents\n",
		".editorconfig":                    "root = true\n",
		".gitattributes":                   "* text=auto eol=lf\n",
		".gitignore":                       "/dist/\n",
		"scripts/verify":                   "#!/bin/sh\nexit 0\n",
		".github/CODEOWNERS":               "* " + ph("Owner") + "\n",
		".github/PULL_REQUEST_TEMPLATE.md": "## What changed\n",
		".github/dependabot.yml":           "version: 2\nupdates: []\n",
		".github/workflows/ci.yml":         "name: CI\non: [push]\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1\n",
		"go.mod":                           "module example.com/demo\n\ngo 1.27\n",
		"go.sum":                           "\n",
		"main.go":                          "package main\n\nfunc main() {}\n",
		"internal/app/app.go":              "package app\n\n// Name is " + ph("ProjectName") + ".\nconst Name = \"" + ph("ProjectName") + "\"\n",
		".repoforge/config.yml":            "# Config for " + ph("ProjectName") + "\nstandard:\n  name: repoforge\n  version: \"" + ph("StandardVersion") + "\"\nrepository:\n  governance_profile: solo\ngithub:\n  issues: true\n  wiki: true\n",
		".repoforge/ownership.yml":         "# Ownership for " + ph("ProjectName") + "\nmanaged_paths:\n  - \".github/workflows/*.yml\"\n",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if name == "scripts/verify" {
			mode = 0o755
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func gitInit(t *testing.T, dir string) error {
	t.Helper()
	cmds := [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "RepoForge Test"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-q", "-m", "chore: initial generated repository"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return err
		} else {
			_ = out
		}
	}
	return nil
}

// TestWikiSyncReportsAnUnreachableWikiCleanly is the regression test for
// the nil-result panic: when Sync cannot read the wiki at all it returns
// no result, and the CLI must report that failure rather than
// dereferencing a nil pointer while printing the plan.
func TestWikiSyncReportsAnUnreachableWikiCleanly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitInit(t, dir); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	var stdout, stderr strings.Builder
	code := run([]string{"wiki", "sync", "--repo", "owner/name"}, &stdout, &stderr)
	// Reaching a return value at all is the guard: the panic aborted the
	// process instead of returning one.
	if code == 0 {
		t.Errorf("an unreachable wiki must not report success:\n%s\n%s", stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "panic") {
		t.Errorf("wiki sync must not panic:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "wiki") {
		t.Errorf("the failure must name what failed:\n%s", stderr.String())
	}
}

// TestWikiSyncRejectsABadSlug proves the usage error path.
func TestWikiSyncRejectsABadSlug(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitInit(t, dir); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	var stdout, stderr strings.Builder
	if code := run([]string{"wiki", "sync", "--repo", "not-a-slug"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2 for a malformed slug", code)
	}
}

// TestPRCheckBodyFlagsReachTheirOwnSubcommand is the regression test for
// a shared flag set: `pr check` and `pr check-body` take different flags,
// and parsing one subcommand's options against the other's set makes the
// body check fail as a usage error on every pull request.
func TestPRCheckBodyFlagsReachTheirOwnSubcommand(t *testing.T) {
	// check-body needs no repository: it validates the body it is given.
	dir := t.TempDir()
	body := filepath.Join(dir, "body.md")
	complete := strings.Join([]string{
		"## What changed", "A thing.", "",
		"## Why", "Closes #3", "",
		"## Specification", "FR-016", "",
		"## Tests", "t", "",
		"## Validation", "gofmt -l .", "",
		"## Risk", "none", "",
		"## Documentation and ADRs", "none", "",
		"## Operational impact", "none", "",
		"## Breaking / migration impact", "None", "",
		"## Known limitations", "none",
	}, "\n")
	if err := os.WriteFile(body, []byte(complete), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if code := run([]string{"pr", "check-body", "--file", body}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0 (a complete body must pass):\n%s\n%s", code, stdout.String(), stderr.String())
	}

	// `pr check` keeps its own flag set: the point is that its --path is
	// still accepted and it does not report a flag error.
	if code := run([]string{"pr", "check"}, &stdout, &stderr); code == 2 {
		t.Errorf("pr check must not be a usage error:\n%s", stderr.String())
	}
}

// TestPRScaffoldHandsTheAgentTheContract proves the scaffold path end
// to end: --issue is mandatory, and the emitted skeleton carries every
// section plus the link, so an agent starting here cannot miss the
// template the way PR #4 did.
func TestPRScaffoldHandsTheAgentTheContract(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := run([]string{"pr", "scaffold"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2 (a scaffold without --issue is a usage error)", code)
	}
	stdout.Reset()
	if code := run([]string{"pr", "scaffold", "--issue", "3"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s\n%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"## What Changed", "## Why", "## Known Limitations", "Closes #3"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("scaffold does not contain %q:\n%s", want, stdout.String())
		}
	}
}

// TestIssueScaffoldAndCheckRoundtrip proves the issue loop from the
// command surface: the chooser is rejected, the raw scaffold fails,
// and the filled scaffold passes.
func TestIssueScaffoldAndCheckRoundtrip(t *testing.T) {
	chdir(t, repoRoot(t))

	var stdout, stderr strings.Builder
	if code := run([]string{"issue", "scaffold", "--type", "config"}, &stdout, &stderr); code != 1 {
		t.Errorf("exit = %d, want 1 (the template chooser is not an issue type)", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"issue", "scaffold", "--type", "bug"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s\n%s", code, stdout.String(), stderr.String())
	}
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw.md")
	if err := os.WriteFile(raw, []byte(stdout.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := run([]string{"issue", "check", "--type", "bug", "--file", raw}, &stdout, &stderr); code != 1 {
		t.Errorf("exit = %d, want 1 (an unfilled scaffold must fail)", code)
	}
	body := strings.ReplaceAll(
		strings.ReplaceAll(
			readTestFile(t, raw),
			"<!-- Fill this in; an empty answer fails validation -->", "answered"),
		"<!-- Pick exactly one of the form's options: safety: the tool mutated or deleted something it must not / security: a credential, permission, or protection boundary is involved / correctness: wrong result, no data loss / usability: confusing output or message -->",
		"correctness: wrong result, no data loss")
	done := filepath.Join(dir, "done.md")
	if err := os.WriteFile(done, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := run([]string{"issue", "check", "--type", "bug", "--file", done}, &stdout, &stderr); code != 0 {
		t.Errorf("exit = %d, want 0 (a filled body must pass):\n%s\n%s", code, stdout.String(), stderr.String())
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestPRReadyGatesTheFullLoop proves the gate agents must pass: a
// topic branch with a conventional commit and a filled scaffold body
// reports OK, and the same branch with the raw scaffold reports not
// ready. Green here is what CI's deterministic checks will see.
func TestPRReadyGatesTheFullLoop(t *testing.T) {
	dir := t.TempDir()
	// Body drafts live outside the repository: the clean-tree rule
	// counts staged-but-uncommitted files as uncommitted.
	paper := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
			"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "--quiet", "-b", "main", ".")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "--quiet", "-m", "chore: base")
	git("checkout", "--quiet", "-b", "fix/7-thing")
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "--quiet", "-m", "fix: a thing")

	var stdout, stderr strings.Builder
	if code := run([]string{"pr", "scaffold", "--issue", "7"}, &stdout, &stderr); code != 0 {
		t.Fatalf("scaffold exit = %d", code)
	}
	raw := filepath.Join(paper, "raw.md")
	if err := os.WriteFile(raw, []byte(stdout.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"pr", "ready", "--path", dir, "--file", raw}, &stdout, &stderr); code != 1 {
		t.Errorf("ready exit = %d, want 1 (the branch is fine but the body is a raw scaffold):\n%s\n%s", code, stdout.String(), stderr.String())
	}

	var lines []string
	for _, line := range strings.Split(readTestFile(t, raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "<!--") {
			lines = append(lines, "answered")
			continue
		}
		lines = append(lines, line)
	}
	done := filepath.Join(paper, "done.md")
	if err := os.WriteFile(done, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"pr", "ready", "--path", dir, "--file", done}, &stdout, &stderr); code != 0 {
		t.Errorf("ready exit = %d, want 0:\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "pr ready: OK") {
		t.Errorf("ready did not report OK:\n%s", stdout.String())
	}
}

// gatedRepo builds a topic branch with one conventional commit ahead
// of main and a clean tree: the shape every create wrapper requires.
func gatedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
			"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "--quiet", "-b", "main", ".")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "--quiet", "-m", "chore: base")
	git("checkout", "--quiet", "-b", "fix/7-thing")
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "--quiet", "-m", "fix: a thing")
	return dir
}

// stubGH installs a gh that records its arguments and succeeds, so
// tests prove what would be delegated without touching the network.
// It returns the bin directory (prepend to PATH) and the log file.
func stubGH(t *testing.T) (bin, log string) {
	t.Helper()
	bin = t.TempDir()
	log = filepath.Join(t.TempDir(), "calls.log")
	script := "#!/bin/sh\necho \"gh $*\" >> \"" + log + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

// withPATH runs f with PATH set to bin plus the system git. The stub
// gh shadows the real one while git keeps working.
func withPATH(t *testing.T, bin string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+filepath.Dir(git))
}

// filledPRScaffold writes a fully answered PR scaffold for issue 7.
func filledPRScaffold(t *testing.T) string {
	t.Helper()
	var out, errOut strings.Builder
	if code := run([]string{"pr", "scaffold", "--issue", "7"}, &out, &errOut); code != 0 {
		t.Fatalf("scaffold exit = %d", code)
	}
	var lines []string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "<!--") {
			lines = append(lines, "answered")
			continue
		}
		lines = append(lines, line)
	}
	done := filepath.Join(t.TempDir(), "pr.md")
	if err := os.WriteFile(done, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	return done
}

// TestPRCreateNeverReachesAGreenGate proves gate-before-delegate: a
// raw scaffold stops the run and the stub gh is never invoked, while
// a filled body on a ready branch delegates exactly once.
func TestPRCreateNeverReachesAGreenGate(t *testing.T) {
	dir := gatedRepo(t)
	bin, log := stubGH(t)
	withPATH(t, bin)

	var stdout, stderr strings.Builder
	if code := run([]string{"pr", "scaffold", "--issue", "7"}, &stdout, &stderr); code != 0 {
		t.Fatalf("scaffold exit = %d", code)
	}
	raw := filepath.Join(t.TempDir(), "raw.md")
	if err := os.WriteFile(raw, []byte(stdout.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"pr", "create", "--title", "fix: a thing", "--file", raw, "--path", dir}, &stdout, &stderr); code != 1 {
		t.Fatalf("create exit = %d, want 1 (raw scaffold must stop before the network)", code)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Errorf("gh was invoked despite a red gate")
	}

	done := filledPRScaffold(t)
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"pr", "create", "--title", "fix: a thing", "--file", done, "--path", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("create exit = %d, want 0:\n%s\n%s", code, stdout.String(), stderr.String())
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("gh was not invoked on a green gate: %v", err)
	}
	if !strings.Contains(string(calls), "pr create --title fix: a thing") {
		t.Errorf("gh was not asked to create the PR:\n%s", calls)
	}
}

// TestPRCreateChecksTheTitle proves the title is validated locally:
// it becomes the squash-merge subject, so a non-conventional title
// fails before the gate even runs.
func TestPRCreateChecksTheTitle(t *testing.T) {
	dir := gatedRepo(t)
	bin, _ := stubGH(t)
	withPATH(t, bin)
	done := filledPRScaffold(t)

	var stdout, stderr strings.Builder
	if code := run([]string{"pr", "create", "--file", done, "--path", dir}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2 (a missing --title is a usage error)", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"pr", "create", "--title", "wip stuff", "--file", done, "--path", dir}, &stdout, &stderr); code != 1 {
		t.Errorf("exit = %d, want 1 (a non-conventional title must fail)", code)
	}
}

// TestIssueCreateDelegatesWithFormLabels proves a filled issue body
// delegates to gh with the form's declared labels attached.
func TestIssueCreateDelegatesWithFormLabels(t *testing.T) {
	chdir(t, repoRoot(t))
	bin, log := stubGH(t)
	withPATH(t, bin)

	body := filepath.Join(t.TempDir(), "issue.md")
	content := "### What happened?\n\nit broke\n\n### Expected behaviour\n\nworks\n\n### Steps to reproduce\n\nrun it\n\n### Severity\n\ncorrectness: wrong result, no data loss\n\n### Affected requirements\n\nunknown\n"
	if err := os.WriteFile(body, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := run([]string{"issue", "create", "--type", "bug", "--title", "t", "--file", body}, &stdout, &stderr); code != 0 {
		t.Fatalf("create exit = %d, want 0:\n%s\n%s", code, stdout.String(), stderr.String())
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("gh was not invoked: %v", err)
	}
	for _, want := range []string{"issue create", "--label", "bug"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("gh invocation is missing %q:\n%s", want, calls)
		}
	}
}

// TestCreateReportsAMissingGH proves a missing gh is reported, never
// silently skipped — with a PATH that contains nothing at all.
func TestCreateReportsAMissingGH(t *testing.T) {
	chdir(t, repoRoot(t))
	body := filepath.Join(t.TempDir(), "issue.md")
	if err := os.WriteFile(body, []byte("### What happened?\n\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr strings.Builder
	if code := run([]string{"issue", "create", "--type", "bug", "--title", "t", "--file", body}, &stdout, &stderr); code != 1 {
		t.Errorf("exit = %d, want 1 (a missing gh must fail loudly)", code)
	}
	if !strings.Contains(stderr.String(), "gh is not installed") {
		t.Errorf("the failure does not name the missing gh:\n%s", stderr.String())
	}
}

// TestHookReportsOutsideARepo proves the hook entry point fails
// closed where there is no repository to install into.
func TestHookReportsOutsideARepo(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := run([]string{"hook", "verify", "--path", t.TempDir()}, &stdout, &stderr); code != 1 {
		t.Errorf("exit = %d, want 1 (no repository, no hooks)", code)
	}
}

// TestReleaseReadyDelegatesToThePolicyScript proves `--tag` is
// mandatory and the policy script's verdict is the command's verdict.
func TestReleaseReadyDelegatesToThePolicyScript(t *testing.T) {
	chdir(t, repoRoot(t))
	var stdout, stderr strings.Builder
	if code := run([]string{"release", "ready"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2 (a missing --tag is a usage error)", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"release", "ready", "--tag", "not-a-version"}, &stdout, &stderr); code == 0 {
		t.Errorf("exit = 0, want non-zero (a malformed tag must fail)")
	}
}

// TestMCPNeedsAServeSubcommand pins the dispatch contract: mcp without
// serve is a usage error that never touches stdin.
func TestMCPNeedsAServeSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := run([]string{"mcp"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2 (mcp needs a subcommand)", code)
	}
	if code := run([]string{"mcp", "bogus"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2 (mcp only serves)", code)
	}
}

// TestMCPServeRefusesWithoutAllowedDir proves the writable root is
// mandatory: no open-ended filesystem access by default.
func TestMCPServeRefusesWithoutAllowedDir(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := runMCP([]string{"serve"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2 (allowed-dir is required)\n%s%s", code, stdout.String(), stderr.String())
	}
}

// TestMCPServeShutsDownCleanOnEOF proves a constructed server exits 0
// when the client goes away with no requests.
func TestMCPServeShutsDownCleanOnEOF(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	var stdout, stderr strings.Builder
	code := runMCP([]string{"serve", "--allowed-dir", dir, "--template-source", root + "/template"},
		strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (clean EOF shutdown)\n%s%s", code, stdout.String(), stderr.String())
	}
}

// TestMCPServeAnswersAFullSession proves the CLI transport end to end:
// initialize, then a capabilities call, then EOF.
func TestMCPServeAnswersAFullSession(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	stdin := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"capabilities","arguments":{}}}` + "\n")
	var stdout, stderr strings.Builder
	code := runMCP([]string{"serve", "--allowed-dir", dir, "--template-source", root + "/template"},
		stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s%s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"protocolVersion":"2025-11-25"`) {
		t.Errorf("handshake missing protocol version: %s", out)
	}
	if !strings.Contains(out, `template_build`) {
		t.Errorf("capabilities missing tool names: %s", out)
	}
}

// TestAstroBuildRendersTheProductTemplate proves the advertised
// product build works end to end from the canonical source.
func TestAstroBuildRendersTheProductTemplate(t *testing.T) {
	root := repoRoot(t)
	chdir(t, root)
	out := filepath.Join(t.TempDir(), "site")
	var stdout, stderr strings.Builder
	args := []string{"astro", "build", "--out", out,
		"--var", "ProjectName=demo-site", "--var", "Description=A demonstration marketing site."}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s%s", code, stdout.String(), stderr.String())
	}
	for _, rel := range []string{"package.json", "src/pages/index.astro", "repoforge.manifest.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("built site missing %s: %v", rel, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(out, "src", "site.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"demo-site"`) {
		t.Errorf("vars not rendered:\n%s", raw)
	}
}

// TestAstroBuildNeedsOutAndVars pins the failure contract: missing
// output and missing values are usage-class failures that write
// nothing.
func TestAstroBuildNeedsOutAndVars(t *testing.T) {
	root := repoRoot(t)
	chdir(t, root)
	var stdout, stderr strings.Builder
	if code := run([]string{"astro", "build"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit = %d, want 2 (missing --out)", code)
	}
	out := filepath.Join(t.TempDir(), "site")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"astro", "build", "--out", out}, &stdout, &stderr); code == 0 {
		t.Errorf("exit = 0, want non-zero (missing vars)")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("failed build must not create %s", out)
	}
}
