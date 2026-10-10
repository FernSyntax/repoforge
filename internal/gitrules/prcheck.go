package gitrules

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CheckPRReady performs the local, GitHub-free pre-flight checks for
// opening a pull request: on a well-named topic branch, at least one
// commit ahead of the default branch, every new commit message valid,
// working tree clean. Read-only: every git invocation is a query.
func CheckPRReady(dir, defaultBranch string) []Issue {
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	var issues []Issue
	add := func(sev Severity, rule, format string, args ...any) {
		issues = append(issues, Issue{Rule: rule, Severity: sev, Message: fmt.Sprintf(format, args...)})
	}

	branch, err := git(dir, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil || branch == "" {
		add(Error, "branch", "detached HEAD; check out a topic branch")
		return issues
	}
	if branch == defaultBranch {
		add(Error, "branch", "on %q; pull requests are opened from topic branches", defaultBranch)
	} else {
		for _, i := range CheckBranchName(branch, defaultBranch) {
			add(i.Severity, "branch", "%s", i.Message)
		}
	}

	ahead, err := git(dir, "rev-list", "--count", defaultBranch+"..HEAD")
	if err != nil {
		add(Error, "commits", "cannot count commits ahead of %q: %v", defaultBranch, err)
		return issues
	}
	if ahead == "0" {
		add(Error, "commits", "no commits ahead of %q", defaultBranch)
	}

	if out, err := git(dir, "log", defaultBranch+"..HEAD", "--format=%h%x09%s"); err == nil && out != "" {
		for _, line := range strings.Split(out, "\n") {
			sha, subject, found := strings.Cut(line, "\t")
			if !found {
				continue
			}
			for _, i := range CheckCommitMessage(subject) {
				if i.Severity == Error {
					add(Error, "commits", "%s: %s", sha, i.Message)
				} else {
					add(Warning, "commits", "%s: %s", sha, i.Message)
				}
			}
		}
	}

	if out, err := git(dir, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
		add(Error, "worktree", "uncommitted changes present; commit or stash before opening a PR")
	}
	return issues
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}
