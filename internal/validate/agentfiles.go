// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// agentFileRefs are the cross-references each agent file must carry.
// The framework works by linking to normative sources, never by
// duplicating them — so a file that stops linking has stopped being
// the framework, and that is a structural failure with a file and a
// line, not a review observation.
var agentFileRefs = map[string][]string{
	"AGENTS.md": {
		"spec.md",
		"DESIGN.md",
	},
	"DESIGN.md": {
		"docs/architecture/",
		"docs/decisions/",
	},
	"template/AGENTS.md": {
		"spec.md",
		"DESIGN.md",
	},
	"template/DESIGN.md": {
		"spec.md",
		"docs/decisions/",
	},
}

// checkAgentFiles verifies the agentic framework: every agent file
// exists in version control and links to its normative sources. It
// runs only in repositories carrying the decision index, mirroring
// checkDecisions: the framework is the operating surface of
// decision-practicing repositories, and minimal managed repositories
// are not penalized for lacking it. Only tracked files count, so the
// check sees the committed framework, never uncommitted drafts.
func checkAgentFiles(root string, trackedSet map[string]bool) []Finding {
	var out []Finding
	if !trackedSet[decisionIndex] {
		return nil
	}
	for _, name := range sortedAgentFileNames() {
		if !trackedSet[name] {
			out = append(out, Finding{Rule: "agents/missing", Path: name,
				Message: "the agentic framework file is missing (or not tracked)"})
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			out = append(out, Finding{Rule: "agents/unreadable", Path: name,
				Message: "cannot read the agentic framework file: " + err.Error()})
			continue
		}
		body := string(data)
		for _, ref := range agentFileRefs[name] {
			if !strings.Contains(body, ref) {
				out = append(out, Finding{Rule: "agents/reference", Path: name,
					Message: "does not reference " + ref + "; agent files link to normative sources, never duplicate them"})
			}
		}
	}
	return out
}

func sortedAgentFileNames() []string {
	out := make([]string, 0, len(agentFileRefs))
	for name := range agentFileRefs {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
