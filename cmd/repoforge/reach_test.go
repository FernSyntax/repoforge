package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseReadyRefusesARepositoryWithoutTheScript pins the wrapper's
// reach check: the policy script is discovered from the repository root,
// and a repository that does not carry it fails cleanly rather than
// running something that is not there.
func TestReleaseReadyRefusesARepositoryWithoutTheScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitInit(t, dir); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI("release", "ready", "--path", dir, "--tag", "v0.1.1")
	if code != 1 || !strings.Contains(errOut, "release-check") {
		t.Fatalf("a repository without the script = %d, want 1 naming it (err=%q)", code, errOut)
	}
}

// TestIssueScaffoldRefusesARepositoryWithoutForms pins the same reach
// contract for the issue forms: the scaffold comes from the repository's
// own forms, and their absence is reported rather than silently
// producing an empty skeleton.
func TestIssueScaffoldRefusesARepositoryWithoutForms(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitInit(t, dir); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI("issue", "scaffold", "--path", dir, "--type", "task")
	if code != 1 || !strings.Contains(errOut, "issue") {
		t.Fatalf("a repository without forms = %d, want 1 naming the failure (err=%q)", code, errOut)
	}
}

// TestPRScaffoldRejectsAnUnknownIssueForm pins the scaffold's type
// gate: an issue form the repository does not declare is an error, not
// an empty skeleton that would sail through the traceability check.
func TestPRScaffoldRejectsAnUnknownIssueForm(t *testing.T) {
	chdir(t, repoRoot(t))
	if code, _, errOut := runCLI("pr", "scaffold", "--issue", "not-a-number"); code != 2 {
		t.Fatalf("a non-numeric issue = %d, want 2 (err=%q)", code, errOut)
	}
}

// TestPRReadyRefusesAMissingBodyFile pins the ready gate's file
// contract: a body file that does not exist is reported as the read
// failure it is, with the filename named.
func TestPRReadyRefusesAMissingBodyFile(t *testing.T) {
	chdir(t, repoRoot(t))
	code, _, errOut := runCLI("pr", "ready", "--file", filepath.Join(t.TempDir(), "absent.md"))
	if code != 2 || !strings.Contains(errOut, "absent.md") {
		t.Fatalf("a missing body = %d, want 2 naming the file (err=%q)", code, errOut)
	}
}
