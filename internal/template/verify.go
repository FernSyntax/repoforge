// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ValidateBuiltTree applies the source rules plus the rules that only a
// generated tree can satisfy: the manifest must exist, be internally
// consistent, and describe exactly the files it sits beside. A built
// tree without a manifest cannot be traced back to the RepoForge
// source that produced it, which is what makes template tampering
// detectable (spec.md §9.1).
//
// A manifest entry with no content is treated as a reserved path: the
// builder validates the tree before the manifest file itself is
// written, then validates the staged tree again afterwards.
func ValidateBuiltTree(files []SourceFile) []Issue {
	issues := ValidateSource(files)
	manifest := findSourceFile(files, ManifestFileName)
	if manifest == nil {
		return append(issues, Issue{
			Path: ManifestFileName, Rule: "manifest/missing",
			Message: "a built template must contain repoforge.manifest.json; rebuild with `repoforge template build`",
		})
	}
	if len(manifest.Data) == 0 {
		return issues
	}
	var m Manifest
	if err := strictJSON(manifest.Data, &m); err != nil {
		return append(issues, Issue{Path: ManifestFileName, Rule: "manifest/invalid", Message: err.Error()})
	}
	issues = append(issues, m.Verify(files)...)
	return issues
}

func findSourceFile(files []SourceFile, rel string) *SourceFile {
	for i := range files {
		if files[i].Rel == rel {
			return &files[i]
		}
	}
	return nil
}

// strictJSON decodes with unknown-field rejection so a manifest that
// gained a field the tool does not understand fails loudly instead of
// silently dropping provenance.
func strictJSON(data []byte, v any) error {
	dec := newJSONDecoder(bytes.NewReader(data))
	return dec.Decode(v)
}

// Verify proves that a built template tree is both internally valid and
// reproducible from the immutable RepoForge source reference recorded in
// its own manifest.
//
// The comparison is deliberately not a self-consistency check alone: a
// hand-edited tree can regenerate a matching manifest, but it cannot
// make the recorded source commit produce the same bytes. Verify
// therefore fetches SourceCommit over git, rebuilds from that exact
// source, and compares the two trees byte-for-byte including file modes
// and the manifest itself.
//
// A manifest with no source commit is a development build. It is
// validated fully but cannot be reproduced from source, so Verify
// reports that as a note unless RequireSource is set — which is what
// template CI uses, because a published template must be traceable.
//
// The returned error reports environmental failures (no git, no
// network, unknown commit); the returned issues report differences.
func Verify(dir, sourceURL string, requireSource bool) ([]Issue, error) {
	files, err := LoadSource(dir)
	if err != nil {
		return nil, err
	}
	if issues := ValidateBuiltTree(files); len(issues) > 0 {
		return issues, nil
	}
	m, err := LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	if sourceURL == "" {
		sourceURL = m.SourceURL
	}
	if !m.Verifiable() {
		if requireSource {
			return nil, fmt.Errorf("template: the manifest records no source_commit and no source_url, so this tree cannot be traced to the RepoForge release that produced it; a published template must record immutable provenance")
		}
		return nil, nil
	}
	if sourceURL == "" {
		return nil, fmt.Errorf("template: the manifest records no source_url; pass --source-url so verification can fetch the recorded commit")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("template: verification needs git to fetch %s: %w", m.SourceCommit, err)
	}

	work, err := os.MkdirTemp("", "repoforge-verify-*")
	if err != nil {
		return nil, fmt.Errorf("template: creating verification workspace: %w", err)
	}
	defer os.RemoveAll(work)

	if err := fetchSource(sourceURL, m.SourceCommit, work); err != nil {
		return nil, err
	}
	rebuilt, err := Build(loadedSource(work), filepath.Join(work, "template"),
		filepath.Join(work, "rebuilt"), nil,
		BuildOptions{
			Force: true,
			Manifest: ManifestOptions{
				RepoForgeVersion: m.RepoForgeVersion,
				SourceRef:        m.SourceRef,
				SourceCommit:     m.SourceCommit,
				// The rebuild must reproduce the recorded build
				// byte-for-byte, so it records the manifest's URL, not
				// the fetch override: --source-url selects where to
				// fetch from (a mirror), never what the manifest says.
				SourceURL: m.SourceURL,
			},
		})
	if err != nil {
		return nil, fmt.Errorf("template: rebuilding from %s: %w", m.SourceCommit, err)
	}
	if len(rebuilt) > 0 {
		return rebuilt, nil
	}
	return diffTrees(files, work)
}

// diffTrees compares the verified directory against the freshly built
// tree, reporting every path whose content or mode differs, including
// files present on only one side.
func diffTrees(have []SourceFile, work string) ([]Issue, error) {
	want, err := LoadSource(filepath.Join(work, "rebuilt"))
	if err != nil {
		return nil, err
	}
	haveByPath := map[string]SourceFile{}
	for _, f := range have {
		haveByPath[f.Rel] = f
	}
	wantByPath := map[string]SourceFile{}
	for _, f := range want {
		wantByPath[f.Rel] = f
	}
	var issues []Issue
	for rel, w := range wantByPath {
		h, ok := haveByPath[rel]
		if !ok {
			issues = append(issues, Issue{Path: rel, Rule: "verify/missing", Message: "absent from the verified tree"})
			continue
		}
		if !bytes.Equal(h.Data, w.Data) {
			issues = append(issues, Issue{Path: rel, Rule: "verify/content", Message: "differs from a fresh build of the recorded source commit"})
			continue
		}
		if h.Mode.Perm() != w.Mode.Perm() {
			issues = append(issues, Issue{Path: rel, Rule: "verify/mode",
				Message: fmt.Sprintf("mode is %#o, the recorded source produces %#o", h.Mode.Perm(), w.Mode.Perm())})
		}
	}
	for rel := range haveByPath {
		if _, ok := wantByPath[rel]; !ok {
			issues = append(issues, Issue{Path: rel, Rule: "verify/extra",
				Message: "present in the verified tree but not produced by the recorded source commit"})
		}
	}
	return issues, nil
}

// fetchSource fetches one commit into a bare-enough work directory.
// The commit is fetched by SHA and checked out detached, so the tree
// that gets rebuilt cannot follow a moved ref.
func fetchSource(url, commit, work string) error {
	run := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("template: git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}
	if err := run("init", "--quiet", work); err != nil {
		return err
	}
	if err := run("remote", "add", "origin", url); err != nil {
		return err
	}
	// A shallow fetch of a single SHA keeps verification cheap and
	// refuses a commit the source no longer serves.
	if err := run("fetch", "--quiet", "--depth", "1", "origin", commit); err != nil {
		return fmt.Errorf("template: fetching %s from %s failed; the recorded source commit is not reachable: %w", commit, url, err)
	}
	if err := run("checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
		return err
	}
	return nil
}

// loadedSource reads the template/ directory of a fetched checkout.
func loadedSource(root string) []SourceFile {
	files, err := LoadSource(filepath.Join(root, "template"))
	if err != nil {
		return nil
	}
	return files
}
