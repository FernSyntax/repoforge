// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// decisionIndex is the ADR index file. Its rows are the machine-checkable
// part of the ADR process (spec.md §20.5): a record that is not indexed
// is a record nobody can find, and an indexed record with no file is a
// dangling reference.
const decisionIndex = "docs/decisions/README.md"

var adrLinkRe = regexp.MustCompile(`\]\((?:\./)?([0-9]{4}-[a-z0-9-]+\.md)\)`)

// checkDecisions verifies the ADR index against the files that exist.
// It runs only when the repository carries a docs/decisions directory,
// so a managed repository without ADRs is not penalised.
func checkDecisions(root string, trackedSet map[string]bool) []Finding {
	var out []Finding
	if !trackedSet[decisionIndex] {
		return nil
	}
	indexed := map[string]string{}
	for _, f := range sortedTracked(trackedSet) {
		if strings.HasPrefix(f, "docs/decisions/") && strings.HasSuffix(f, ".md") && f != decisionIndex {
			indexed[filepath.Base(f)] = f
		}
	}

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(decisionIndex)))
	if err != nil {
		return append(out, Finding{Rule: "decisions/index", Path: decisionIndex,
			Message: "cannot read the ADR index: " + err.Error()})
	}
	linked := map[string]bool{}
	for _, m := range adrLinkRe.FindAllStringSubmatch(string(data), -1) {
		name := m[1]
		linked[name] = true
		if _, ok := indexed[name]; !ok {
			out = append(out, Finding{Rule: "decisions/broken-reference", Path: decisionIndex,
				Message: "indexes " + name + ", which does not exist under docs/decisions/"})
		}
	}
	for _, name := range sortedRecordNames(indexed) {
		if !linked[name] {
			out = append(out, Finding{Rule: "decisions/unindexed", Path: indexed[name],
				Message: "ADR is not listed in " + decisionIndex + "; add a row with its status and spec sections"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func sortedTracked(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedRecordNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
