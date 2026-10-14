// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package gitrules

import (
	"os"
	"strings"
	"testing"
)

// TestPRCheckDefaultsToMain pins the documented default: an empty default
// branch means main, so a caller that does not know the repository's
// default still gets a meaningful check rather than an empty comparison.
func TestPRCheckDefaultsToMain(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "-c", "feat/1-add-thing")
	if err := os.WriteFile(dir+"/thing.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "feat: add thing")

	issues := CheckPRReady(dir, "")
	if Failed(issues) {
		t.Fatalf("an empty default branch must mean main, got: %s", joinMessages(issues))
	}
	// On the default branch itself the check must fail, which proves the
	// default was really applied rather than the comparison being skipped.
	gitRun(t, dir, "switch", "main")
	if issues := CheckPRReady(dir, ""); !Failed(issues) {
		t.Error("with main as the default, being on main must fail")
	}
}

// TestPRCheckReportsADetachedHead pins the first gate. A detached HEAD has
// no branch to open a pull request from, so the check stops there rather
// than reporting a pile of downstream findings that do not apply.
func TestPRCheckReportsADetachedHead(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "--detach", "HEAD")

	issues := CheckPRReady(dir, "main")
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want exactly the detached-HEAD finding", issues)
	}
	if issues[0].Rule != "branch" || !strings.Contains(issues[0].Message, "detached HEAD") {
		t.Errorf("issues[0] = %+v, want a detached-HEAD branch finding", issues[0])
	}
}

// TestPRCheckReportsAnUnknowableDefaultBranch pins the failure an operator
// hits when the default branch does not exist locally: the commit count
// cannot be resolved, so the check says so and stops instead of reporting
// "no commits ahead" for a comparison that never ran.
func TestPRCheckReportsAnUnknowableDefaultBranch(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "-c", "feat/1-add-thing")
	if err := os.WriteFile(dir+"/thing.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "feat: add thing")

	issues := CheckPRReady(dir, "no-such-branch")
	if !Failed(issues) {
		t.Fatal("an unresolvable default branch must fail the check")
	}
	found := false
	for _, i := range issues {
		if i.Rule == "commits" && strings.Contains(i.Message, "cannot count commits ahead") {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want a 'cannot count commits ahead' finding", issues)
	}
}

// TestPRCheckSurfacesCommitWarningsAsWarnings pins the severity split in
// the commit loop: style guidance is reported at Warning and never fails
// the gate, because a hard style gate once held a merged main red over a
// single character.
func TestPRCheckSurfacesCommitWarningsAsWarnings(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "-c", "feat/1-add-thing")
	if err := os.WriteFile(dir+"/thing.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	// A non-imperative subject is style guidance (Warning), while a
	// trailing period would be a hard Error.
	gitRun(t, dir, "commit", "-m", "feat: added thing")

	issues := CheckPRReady(dir, "main")
	if Failed(issues) {
		t.Fatalf("a style warning must not fail the gate: %s", joinMessages(issues))
	}
	found := false
	for _, i := range issues {
		if i.Severity == Warning && i.Rule == "commits" {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want the imperative-mood warning surfaced at Warning severity", issues)
	}
}

// TestPRCheckSurfacesCommitErrorsAsErrors pins the other half of the
// severity split: a genuine commit-message violation is reported at Error
// and does fail the gate.
func TestPRCheckSurfacesCommitErrorsAsErrors(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "-c", "feat/1-add-thing")
	if err := os.WriteFile(dir+"/thing.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "added thing.")

	issues := CheckPRReady(dir, "main")
	if !Failed(issues) {
		t.Fatal("an invalid commit message must fail the gate")
	}
	found := false
	for _, i := range issues {
		if i.Severity == Error && i.Rule == "commits" {
			found = true
		}
	}
	if !found {
		t.Errorf("issues = %+v, want the commit-message violation surfaced at Error severity", issues)
	}
}

// TestGitReportsAFailingInvocation pins the helper's error path: a git
// command that fails returns an error rather than empty output, because
// the callers distinguish "ran and produced nothing" from "could not run".
func TestGitReportsAFailingInvocation(t *testing.T) {
	if _, err := git(t.TempDir(), "definitely-not-a-git-subcommand"); err == nil {
		t.Fatal("an invalid git subcommand must return an error")
	}
}

// TestGitTrimsTrailingNewlines pins the output shape every caller relies
// on: a command's trailing newline is stripped, so a comparison against a
// branch name or a commit count is exact.
func TestGitTrimsTrailingNewlines(t *testing.T) {
	dir := initPRRepo(t)
	got, err := git(dir, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil {
		t.Fatalf("git: %v", err)
	}
	if got != "main" {
		t.Errorf("git = %q, want %q with no trailing newline", got, "main")
	}
}
