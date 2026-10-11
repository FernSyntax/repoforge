// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"fmt"
	"strings"

	"github.com/FernSyntax/repoforge/internal/config"
	"github.com/FernSyntax/repoforge/internal/gitrules"
)

func checkBranch(root string, pol *config.Policy) []Finding {
	branch, err := gitOutput(root, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil || branch == "" {
		return nil // detached HEAD: nothing to check
	}
	defaultBranch := "main"
	if pol != nil {
		defaultBranch = pol.DefaultBranch()
	}
	var findings []Finding
	for _, i := range gitrules.CheckBranchName(branch, defaultBranch) {
		if i.Severity == gitrules.Error {
			findings = append(findings, Finding{Rule: "branch/name", Path: branch, Message: i.Message})
		}
	}
	return findings
}

// checkCommits validates the subjects of the n most recent commits.
// A repository without commits is valid (nothing to inspect).
func checkCommits(root string, n int, pol *config.Policy) []Finding {
	out, err := gitOutput(root, "log", fmt.Sprintf("-%d", n), "--format=%h%x09%s")
	if err != nil {
		return nil // no commits
	}
	var findings []Finding
	for _, line := range strings.Split(out, "\n") {
		sha, subject, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		for _, i := range gitrules.CheckCommitMessage(subject) {
			if i.Severity == gitrules.Error {
				findings = append(findings, Finding{Rule: "commit/message", Path: "commit " + sha, Message: i.Message})
			}
		}
	}
	return findings
}
