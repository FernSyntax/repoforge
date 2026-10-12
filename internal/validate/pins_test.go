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
	"testing"
)

// usesRe matches an action reference in a workflow: owner/repo/path@<40 hex>.
var usesRe = regexp.MustCompile(`uses:\s*([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_./-]+)*)@([0-9a-f]{40})`)

// workflowDirs are the directories that may carry workflow files. Both
// the engine's own .github/workflows and the template's, because the
// template's workflows are published content and a broken pin there
// fails the generated repository's CI.
var workflowDirs = []string{".github/workflows", "template/.github/workflows"}

// actionPins returns owner/repo@sha for every pinned action in the
// repository's workflow files.
func actionPins(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, dir := range workflowDirs {
		entries, err := os.ReadDir(filepath.Join("..", "..", dir))
		if err != nil {
			continue // a repository without that directory is fine
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			data, err := os.ReadFile(filepath.Join("..", "..", dir, e.Name()))
			if err != nil {
				continue
			}
			for _, m := range usesRe.FindAllStringSubmatch(string(data), -1) {
				out = append(out, m[1]+"@"+m[2])
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestActionPinsResolveInCI is the regression test for a whole class of
// release-blocking defect: a transposed digit in a full-length commit
// SHA.
//
// The failure this prevents is invisible locally. `actionlint` parses
// workflow syntax but does not resolve SHAs, and every local gate still
// passes. GitHub Actions then refuses to start the job at all with
// "Unable to resolve action X, unable to find version <sha>", so a
// required check never reports and the pipeline deadlocks.
//
// Resolving every SHA against the live API needs network, which a unit
// test must not depend on. The property that IS checkable offline, and
// that catches exactly the transposition, is that each action is pinned
// to exactly one SHA everywhere it is referenced. A typo produces a
// second, different SHA for the same action, and the workflow that uses
// it fails to start.
func TestActionPinsResolveInCI(t *testing.T) {
	pins := actionPins(t)
	if len(pins) == 0 {
		t.Fatal("no action pins found; the repository must pin third-party actions")
	}
	shas := map[string][]string{} // action -> SHA list
	for _, pin := range pins {
		action, sha, _ := strings.Cut(pin, "@")
		shas[action] = append(shas[action], sha)
	}
	for action, list := range shas {
		unique := map[string]bool{}
		for _, sha := range list {
			unique[sha] = true
		}
		if len(unique) > 1 {
			variants := make([]string, 0, len(unique))
			for sha := range unique {
				variants = append(variants, sha)
			}
			sort.Strings(variants)
			t.Errorf("action %s is pinned to %d different commit SHAs: %v", action, len(unique), variants)
		}
	}
	// Every pin must be a full-length lowercase hex digest: an abbreviated
	// pin is mutable by its author, which is what pinning exists to stop.
	for _, pin := range pins {
		_, sha, _ := strings.Cut(pin, "@")
		if len(sha) != 40 {
			t.Errorf("pin %s is not a full-length commit SHA", pin)
		}
		for _, c := range sha {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				t.Errorf("pin %s contains %q; SHAs are lowercase hex", pin, string(c))
			}
		}
	}
}

// reviewedActions is the allowlist of third-party actions this
// repository deliberately uses. A new action is a supply-chain decision
// and must be added here, so it is never a silent edit.
//
// Deliberately a list of ACTION NAMES, not SHAs. A hardcoded SHA map
// looks harmless and is actively harmful in two ways: it blocks every
// legitimate pin upgrade (so every Dependabot bump fails the build), and
// the constant 40-hex strings trip gitleaks' generic-api-key rule,
// failing secret scanning on a test file that contains no secret.
var reviewedActions = map[string]bool{
	"actions/attest":                    true,
	"actions/checkout":                  true,
	"actions/create-github-app-token":   true,
	"actions/dependency-review-action":  true,
	"actions/download-artifact":         true,
	"actions/setup-go":                  true,
	"actions/upload-artifact":           true,
	"gitleaks/gitleaks-action":          true,
	"goreleaser/goreleaser-action":      true,
	"github/codeql-action/upload-sarif": true,
	"ossf/scorecard-action":             true,
}

// TestActionSetIsReviewed proves no unvetted third-party action has
// entered the workflows.
func TestActionSetIsReviewed(t *testing.T) {
	for _, pin := range actionPins(t) {
		action, _, _ := strings.Cut(pin, "@")
		if !reviewedActions[action] {
			t.Errorf("action %s is not in the reviewed allowlist; adding a third-party action is a supply-chain decision — add it to reviewedActions deliberately", action)
		}
	}
}

// releaseWorkflowFile is the workflow under test.
const releaseWorkflowFile = ".github/workflows/release.yml"

// TestReleaseWorkflowNeverDirtsTheTree is the regression test for the
// release that never published.
//
// GoReleaser refuses to build a dirty git tree, and it reports it as:
//
//	error: git is in a dirty state
//	Please check in your pipeline what can be changing the following files:
//	?? notes.md
//
// A file written into the checkout by the release pipeline itself is
// therefore a release blocker, and it is invisible to every other gate:
// gofmt, vet, tests, validate, actionlint, and shellcheck all pass. Only
// the hosted run catches it, and it catches it after the tag is pushed,
// which is the worst possible time.
//
// The guard is structural: no step in the release workflow may redirect
// output to a bare relative path inside the checkout. A runner-scoped
// temporary directory is the only correct destination.
func TestReleaseWorkflowNeverDirtsTheTree(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", releaseWorkflowFile))
	if err != nil {
		t.Fatalf("cannot read %s: %v", releaseWorkflowFile, err)
	}
	// A bare relative path: no leading `$`, no `/`. That is the shape of
	// `> notes.md`, the exact bug. Runner-scoped and absolute targets
	// ($RUNNER_TEMP, $GITHUB_OUTPUT, /dev/null) are all fine.
	dirtyRedirect := regexp.MustCompile(`(?m)>>?\s*([A-Za-z0-9._-][A-Za-z0-9._/-]*)\s*$`)
	for _, m := range dirtyRedirect.FindAllStringSubmatch(string(data), -1) {
		target := m[1]
		if target == "/dev/null" || strings.HasPrefix(target, "$") || strings.HasPrefix(target, "/") {
			continue
		}
		t.Errorf("%s writes to the relative path %q; GoReleaser refuses a dirty tree and the release fails with \"git is in a dirty state\" — write to $RUNNER_TEMP instead", releaseWorkflowFile, target)
	}
}

// TestReleaseNotesGoToARunnerTempPath is the positive half: the notes
// extraction must demonstrably use RUNNER_TEMP, so the guard above
// cannot pass by the step being absent.
func TestReleaseNotesGoToARunnerTempPath(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", releaseWorkflowFile))
	if err != nil {
		t.Fatalf("cannot read %s: %v", releaseWorkflowFile, err)
	}
	body := string(data)
	if !strings.Contains(body, "release-notes.sh") {
		t.Fatal("the release workflow must extract the notes from CHANGELOG.md")
	}
	if !strings.Contains(body, "> \"$RUNNER_TEMP/notes.md\"") {
		t.Error("the release notes must be written to ${RUNNER_TEMP}/notes.md, outside the checkout")
	}
	if !strings.Contains(body, "${{ runner.temp }}/notes.md") {
		t.Error("the notes artifact must be staged from runner.temp")
	}
}

// TestChecksumVerificationRunsFromDist is the regression test for the
// release that failed on its final step.
//
// GoReleaser writes checksums.txt with bare file names. Running
// `sha256sum -c dist/checksums.txt` from the repository root resolves
// them against that root, so every asset reports:
//
//	sha256sum: repoforge_0.1.0_linux_amd64.tar.gz: No such file or directory
//	repoforge_0.1.0_linux_amd64.tar.gz: FAILED open or read
//
// The release was already created and its 11 assets already uploaded at
// that point, so the failure is purely in the verification step — and it
// turns a successful release red.
//
// This is also the test that guards against a claimed fix that is not
// present. The correction was once reverted by an unrelated
// `git checkout -- .github/` while a commit message still described it,
// so the assertion is against the workflow text, not against intent.
func TestChecksumVerificationRunsFromDist(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", releaseWorkflowFile))
	if err != nil {
		t.Fatalf("cannot read %s: %v", releaseWorkflowFile, err)
	}
	body := string(data)
	bad := "sha256sum -c dist/checksums.txt"
	if strings.Contains(body, bad) {
		t.Errorf("%s contains %q; checksums.txt lists bare file names, so the check must run from inside dist/ or every asset reports FAILED open or read", releaseWorkflowFile, bad)
	}
	good := "(cd dist && sha256sum -c checksums.txt)"
	if !strings.Contains(body, good) {
		t.Errorf("%s does not contain %q; the checksum check must run from inside dist/", releaseWorkflowFile, good)
	}
}

// TestEveryWorkflowChecksumCheckIsDirectoryRelative guards the same
// mistake anywhere else it could appear, including the template's
// workflows, which are published content.
func TestEveryWorkflowChecksumCheckIsDirectoryRelative(t *testing.T) {
	for _, dir := range workflowDirs {
		entries, err := os.ReadDir(filepath.Join("..", "..", dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join("..", "..", dir, e.Name()))
			if err != nil {
				continue
			}
			if strings.Contains(string(data), "sha256sum -c dist/checksums.txt") {
				t.Errorf("%s/%s checks checksums.txt from the repository root; run it from inside dist/", dir, e.Name())
			}
		}
	}
}
