// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"github.com/FernSyntax/repoforge/internal/config"
)

// requiredFiles is the standard's minimal baseline file set every
// managed repository must carry (tracked, not merely present).
var requiredFiles = []string{"README.md", "LICENSE", "CHANGELOG.md"}

func checkRequiredFiles(tracked map[string]bool) []Finding {
	var out []Finding
	for _, name := range requiredFiles {
		if !tracked[name] {
			out = append(out, Finding{
				Rule:    "files/required",
				Path:    name,
				Message: "required baseline file missing (or not tracked)",
			})
		}
	}
	return out
}

// checkOwnership verifies that every managed path pattern matches at
// least one tracked file. Pattern validity and cross-list duplicates
// are validated in internal/config; glob semantics are path.Match
// anchored at the repository root.
func checkOwnership(pol *config.Policy, tracked []string) []Finding {
	var out []Finding
	for _, pattern := range pol.Ownership.ManagedPaths {
		matched := false
		for _, f := range tracked {
			if ok, err := config.MatchPattern(pattern, f); err == nil && ok {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, Finding{
				Rule:    "files/ownership",
				Path:    pattern,
				Message: "managed path pattern matches no tracked files",
			})
		}
	}
	return out
}
