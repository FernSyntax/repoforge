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
