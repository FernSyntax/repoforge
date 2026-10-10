package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/schematools"
)

// repoRoot is the engine checkout, resolved relative to this file so the
// tests work from any working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// runScript runs scripts/verify and returns its exit code and output.
func runScript(t *testing.T, dir string, env []string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(repoRoot(t), "scripts", "verify"), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), out.String(), errOut.String()
	}
	if err != nil {
		t.Fatalf("running scripts/verify %v: %v", args, err)
	}
	return 0, out.String(), errOut.String()
}

// TestVerifyHelpProvesUsage pins the contract that --help prints usage
// and exits 0, so a developer can discover the interface.
func TestVerifyHelpProvesUsage(t *testing.T) {
	code, out, _ := runScript(t, t.TempDir(), nil, "--help")
	if code != 0 {
		t.Fatalf("--help exit = %d, want 0", code)
	}
	for _, want := range []string{"fast", "full", "--with-act", "--with-integration", "build-template"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage does not mention %q:\n%s", want, out)
		}
	}
}

// TestVerifyRejectsUnknownMode pins the exit-2 contract for a usage
// error and that the message names the accepted modes.
func TestVerifyRejectsUnknownMode(t *testing.T) {
	code, _, errOut := runScript(t, t.TempDir(), nil, "nonsense")
	if code != 2 {
		t.Errorf("exit = %d, want 2 for a usage error", code)
	}
	if !strings.Contains(errOut, "unknown argument") {
		t.Errorf("stderr missing the reason:\n%s", errOut)
	}
}

// TestVerifyOptInWithoutPrerequisiteIsAUsageError proves an opt-in that
// cannot run is refused with exit 2 and the exact configuration needed,
// never silently skipped.
func TestVerifyOptInWithoutPrerequisiteIsAUsageError(t *testing.T) {
	code, _, errOut := runScript(t, t.TempDir(), []string{"REPOFORGE_TEST_REPO="}, "fast", "--with-integration")
	if code != 2 {
		t.Errorf("exit = %d, want 2 when REPOFORGE_TEST_REPO is unset", code)
	}
	for _, want := range []string{"REPOFORGE_TEST_REPO", "disposable"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
}

// TestVerifyRequiresAWellFormedTestRepoSlug pins the second guard on the
// same opt-in: a malformed slug is a usage error.
func TestVerifyRequiresAWellFormedTestRepoSlug(t *testing.T) {
	code, _, errOut := runScript(t, t.TempDir(), []string{"REPOFORGE_TEST_REPO=not-a-slug"}, "fast", "--with-integration")
	if code != 2 {
		t.Errorf("exit = %d, want 2 for a malformed slug", code)
	}
	if !strings.Contains(errOut, "owner/name") {
		t.Errorf("stderr missing the expected form:\n%s", errOut)
	}
}

// TestVerifyWorksFromAnyDirectory proves the script resolves the
// repository root rather than trusting $PWD. --help is used because it
// exercises the resolution without running the suite.
func TestVerifyWorksFromAnyDirectory(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	code, out, _ := runScript(t, dir, nil, "--help")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 when run from %s", code, dir)
	}
	if !strings.Contains(out, "verify") {
		t.Errorf("usage output unexpected:\n%s", out)
	}
	// Direct invocation from an unrelated directory must also work.
	cmd := exec.Command(filepath.Join(root, "scripts", "verify"), "--help")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running from %s: %v\n%s", dir, err, out)
	}
}

// TestCheckSchemaParityPasses covers the parity guard that the
// pre-commit hook runs.
func TestCheckSchemaParityPasses(t *testing.T) {
	cmd := exec.Command(filepath.Join(repoRoot(t), "scripts", "check-schema-parity"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("check-schema-parity: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "schemas/repoforge.schema.yaml ==") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// TestCheckSchemaParityPassesOnTheRealTree proves the authoritative
// script succeeds against the shipped schemas.
func TestCheckSchemaParityPassesOnTheRealTree(t *testing.T) {
	cmd := exec.Command(filepath.Join(repoRoot(t), "scripts", "check-schema-parity"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("check-schema-parity: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "schema check: 2 schema pair(s) identical") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// TestSchemaParityDriftIsDetected proves drift fails rather than passing
// silently. The comparison itself is internal/schematools, which the
// script invokes; a sandbox copy of the script alone cannot run it
// because it needs the Go module, so the drift is exercised at the layer
// that implements it.
func TestSchemaParityDriftIsDetected(t *testing.T) {
	dir := t.TempDir()
	y := filepath.Join(dir, "a.yaml")
	j := filepath.Join(dir, "a.json")
	if err := os.WriteFile(y, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := schematools.Check(schematools.Pair{YAML: y, JSON: j}); err != nil {
		t.Errorf("an identical pair must pass: %v", err)
	}
	if err := os.WriteFile(j, []byte(`{"a":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := schematools.Check(schematools.Pair{YAML: y, JSON: j})
	if err == nil {
		t.Fatal("drift must fail")
	}
	if !strings.Contains(err.Error(), "differs") {
		t.Errorf("the failure must name the drift: %v", err)
	}
}

// TestCheckIssueFormsPasses covers the guard that rejects a form GitHub
// would silently refuse to render.
func TestCheckIssueFormsPasses(t *testing.T) {
	cmd := exec.Command(filepath.Join(repoRoot(t), "scripts", "check-issue-forms"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("check-issue-forms: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "issue form(s) and the PR template are valid") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// TestCheckIssueFormsIsLoudOnDrift proves an unsupported field type is
// reported with the form and the item named, and that the real
// repository is left untouched.
func TestCheckIssueFormsIsLoudOnDrift(t *testing.T) {
	sandbox := t.TempDir()
	for _, rel := range []string{
		"scripts/check-issue-forms",
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/ISSUE_TEMPLATE/bug.yml",
		".github/ISSUE_TEMPLATE/config.yml",
	} {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(sandbox, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// An unsupported body-item type is exactly the failure that produced
	// an unrenderable form before.
	victim := filepath.Join(sandbox, ".github", "ISSUE_TEMPLATE", "bug.yml")
	broken, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(broken), "type: textarea", "type: telepathy", 1)
	if patched == string(broken) {
		t.Fatal("the fixture did not contain a textarea item")
	}
	if err := os.WriteFile(victim, []byte(patched), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(filepath.Join(sandbox, "scripts", "check-issue-forms"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("a form with an unsupported type must fail")
	}
	if !strings.Contains(string(out), "unsupported type") {
		t.Errorf("failure does not name the problem:\n%s", out)
	}
	if !strings.Contains(string(out), "bug.yml") {
		t.Errorf("failure does not name the form:\n%s", out)
	}

	cmd = exec.Command("git", "status", "--porcelain", ".github")
	cmd.Dir = repoRoot(t)
	status, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 0 {
		t.Errorf("the real forms were modified: %s", status)
	}
}
