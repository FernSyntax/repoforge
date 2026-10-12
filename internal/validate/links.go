// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownLinkRe = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)\)`)

// checkMarkdownLinks verifies that relative links in tracked Markdown
// files resolve to existing files or directories. External URLs,
// anchors, and mailto links are out of scope ("where practical").
func checkMarkdownLinks(root string, tracked []string) []Finding {
	var out []Finding
	for _, f := range tracked {
		if !strings.HasSuffix(strings.ToLower(f), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		for _, m := range markdownLinkRe.FindAllSubmatch(data, -1) {
			target := string(m[1])
			if broken := checkLinkTarget(root, f, target); broken != "" {
				out = append(out, Finding{Rule: "docs/link", Path: f, Message: broken})
			}
		}
	}
	return out
}

func checkLinkTarget(root, mdFile, target string) string {
	if target == "" || strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return ""
	}
	target = strings.SplitN(target, "#", 2)[0]
	if target == "" {
		return ""
	}
	target = strings.ReplaceAll(target, "%20", " ")
	resolved := path.Join(path.Dir(mdFile), target)
	if strings.HasPrefix(resolved, "..") {
		return fmt.Sprintf("link %q escapes the repository root", target)
	}
	if _, err := os.Stat(filepath.Join(root, resolved)); err != nil {
		return fmt.Sprintf("broken internal link %q", target)
	}
	return ""
}
