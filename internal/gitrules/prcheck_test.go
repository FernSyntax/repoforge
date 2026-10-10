package gitrules

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
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

func initPRRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "chore: initial commit")
	return dir
}

func TestPRCheckReadyBranch(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "-c", "feat/1-add-thing")
	if err := os.WriteFile(filepath.Join(dir, "thing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "feat: add thing")
	if issues := CheckPRReady(dir, "main"); Failed(issues) {
		t.Fatalf("expected ready, got: %+v", issues)
	}
}

func TestPRCheckOnMain(t *testing.T) {
	dir := initPRRepo(t)
	issues := CheckPRReady(dir, "main")
	if !Failed(issues) {
		t.Fatal("expected failure on default branch")
	}
}

func TestPRCheckNoCommitsAhead(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "-c", "feat/1-add-thing")
	issues := CheckPRReady(dir, "main")
	if !Failed(issues) {
		t.Fatal("expected failure with no commits ahead")
	}
}

func TestPRCheckBadCommitMessage(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "-c", "feat/1-add-thing")
	if err := os.WriteFile(filepath.Join(dir, "thing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "this is not conventional")
	issues := CheckPRReady(dir, "main")
	if !Failed(issues) {
		t.Fatal("expected failure for non-conventional commit")
	}
}

func TestPRCheckBadBranchName(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "-c", "feature")
	if err := os.WriteFile(filepath.Join(dir, "thing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "feat: add thing")
	issues := CheckPRReady(dir, "main")
	if !Failed(issues) {
		t.Fatal("expected failure for bad branch name")
	}
}

func TestPRCheckDirtyWorktree(t *testing.T) {
	dir := initPRRepo(t)
	gitRun(t, dir, "switch", "-c", "feat/1-add-thing")
	if err := os.WriteFile(filepath.Join(dir, "thing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "feat: add thing")
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := CheckPRReady(dir, "main")
	if !Failed(issues) {
		t.Fatal("expected failure for dirty worktree")
	}
}
