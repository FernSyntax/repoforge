// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/FernSyntax/repoforge/internal/brand"
	"github.com/FernSyntax/repoforge/internal/config"
)

// checkContent scans tracked text files for unresolved template
// placeholders, token/key-looking material, absolute machine paths, and
// legacy product branding. Scans are size-bounded and skip binary
// files; findings are identical for identical content (spec.md §12.2).
func checkContent(root string, tracked []string, pol *config.Policy) []Finding {
	var out []Finding
	for _, f := range tracked {
		data, ok := readTextFile(root, f)
		if !ok {
			continue
		}
		if !isPlaceholderAllowed(pol, f) {
			if m := placeholderRe.Find(data); m != nil {
				out = append(out, Finding{Rule: "content/placeholder", Path: f, Message: fmt.Sprintf("unresolved template placeholder %q (bootstrap must resolve these; exempt paths belong in ownership.placeholder_paths)", m)})
			}
		}
		for _, pat := range secretPatterns {
			if pat.re.Match(data) {
				out = append(out, Finding{Rule: "content/secret", Path: f, Message: fmt.Sprintf("contains material matching %s; never commit credentials or keys (spec.md §8)", pat.name)})
			}
		}
		for _, pat := range absPathPatterns {
			if m := pat.Find(data); m != nil {
				out = append(out, Finding{Rule: "content/abs-path", Path: f, Message: fmt.Sprintf("contains absolute machine path %q; use repository-relative paths", m)})
			}
		}
		// RepoForge v0.1.0 begins a fresh product history: the legacy
		// product name, module path, and repository URLs must not
		// survive anywhere in the tracked tree.
		for _, msg := range brand.Find(string(data)) {
			out = append(out, Finding{Rule: "content/branding", Path: f, Message: msg})
		}
	}
	return out
}

const maxScanSize = 1 << 20 // 1 MiB

func readTextFile(root, name string) ([]byte, bool) {
	info, err := os.Stat(filepath.Join(root, name))
	if err != nil || info.IsDir() || info.Size() > maxScanSize {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return nil, false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, false // binary
	}
	return data, true
}

// placeholderRe matches RepoForge's own placeholder syntax: a dot, an
// identifier, and closing braces, with no expression in between.
//
// The dot is load-bearing. GitHub Actions writes ${{ ... }} with an
// expression, never a dotted identifier, so requiring the dot is what
// keeps a workflow file from looking like an unresolved template.
// GoReleaser's variables do share the shape and stay exempt through
// ownership.placeholder_paths, which is the honest place for them
// because they genuinely are a different tool's variables.
var placeholderRe = regexp.MustCompile(`\{\{\s*\.[A-Za-z][A-Za-z0-9]*\s*\}\}`)

func isPlaceholderAllowed(pol *config.Policy, name string) bool {
	if pol == nil {
		return false
	}
	for _, pattern := range pol.Ownership.PlaceholderPaths {
		if ok, _ := config.MatchPattern(pattern, name); ok {
			return true
		}
	}
	return false
}

var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"GitHub personal access token", regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)},
	{"GitHub fine-grained token", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`)},
	{"GitHub OAuth/app/refresh token", regexp.MustCompile(`(?:gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}`)},
	{"private key block", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----`)},
	{"AWS access key ID", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"Slack token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
}

var absPathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`/(?:home|Users)/[A-Za-z0-9._-]+/`),
	regexp.MustCompile(`[A-Za-z]:\\Users\\[^\s\\]+`),
}
