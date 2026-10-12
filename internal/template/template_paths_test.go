// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/brand"
)

// TestIssueStringPinsBothShapes proves an issue renders with its path
// when it has one and without one when it does not, so an operator
// reading CLI output is never shown an empty "path:" prefix for a
// tree-wide finding.
func TestIssueStringPinsBothShapes(t *testing.T) {
	if got := (Issue{Rule: "template/required", Message: "missing"}).String(); got != "[template/required] missing" {
		t.Errorf("pathless issue: %q", got)
	}
	if got := (Issue{Path: "README.md", Rule: "template/required", Message: "missing"}).String(); got != "README.md: [template/required] missing" {
		t.Errorf("pathful issue: %q", got)
	}
}

// TestLoadSourceRefusesMissingAndFileTargets pins that a source named
// but not present is reported as a load failure instead of an empty
// tree, and that an entry which cannot be read aborts the load rather
// than silently dropping a file from the generated tree.
func TestLoadSourceRefusesMissingAndFileTargets(t *testing.T) {
	if _, err := LoadSource(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a nonexistent source directory must fail to load")
	}
	// A dangling symlink whose target's parent does not exist: Lstat
	// succeeds, so the walk reaches the file and ReadFile fails.
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling.md")
	if err := os.Symlink(filepath.Join(dir, "missing", "nowhere.md"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSource(dir); err == nil {
		t.Error("an unreadable entry must fail the load, not be skipped")
	}
}

// TestLoadSourceSkipsGit pins that the repository's own metadata is
// never template material: a .git directory in the source tree is
// skipped entirely, so a checkout used directly as a source cannot
// leak its history into a generated repository.
func TestLoadSourceSkipsGit(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"README.md", ".git/config", ".git/objects/ab/cdef"} {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Rel, ".git/") || f.Rel == ".git" {
			t.Fatalf("repository metadata leaked into the source tree: %s", f.Rel)
		}
	}
	if len(files) != 1 || files[0].Rel != "README.md" {
		t.Errorf("only README.md must load, got %+v", files)
	}
}

// TestValidateSourceFlagsEveryRemainingRule covers the source rules
// with no failing case yet: branding, machine-specific absolute paths,
// unparseable YAML in both .github/ and the ownership policy, and a
// non-executable scripts/verify. Each rule pins a defect that would
// otherwise ship into every generated repository.
func TestValidateSourceFlagsEveryRemainingRule(t *testing.T) {
	for _, tc := range []struct {
		name string
		rel  string
		data string
		rule string
	}{
		{
			name: "legacy branding",
			rel:  "README.md",
			data: "Built from " + brand.Normalize("Repo", "Doctrine") + ".\n",
			rule: "template/branding",
		},
		{
			name: "machine-specific absolute path",
			rel:  "README.md",
			data: "checkout lives at /home/operator/src\n",
			rule: "template/abs-path",
		},
		{
			name: "unparseable workflow YAML",
			rel:  ".github/workflows/ci.yml",
			data: "name: CI\njobs: [unclosed\n",
			rule: "template/yaml",
		},
		{
			name: "unparseable ownership policy",
			rel:  ".repoforge/ownership.yml",
			data: "managed_paths: [unclosed\n",
			rule: "template/yaml",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := loadFixture(t)
			for i := range files {
				if files[i].Rel == tc.rel {
					files[i].Data = []byte(tc.data)
				}
			}
			if !hasRule(ValidateSource(files), tc.rule) {
				t.Fatalf("expected %s issue, got %+v", tc.rule, ValidateSource(files))
			}
		})
	}
}

// TestValidateSourceFlagsNonExecutableVerify pins the executable-bit
// rule: a generated repository whose verify script lost its mode
// cannot run the standard's own verification.
func TestValidateSourceFlagsNonExecutableVerify(t *testing.T) {
	files := loadFixture(t)
	for i := range files {
		if files[i].Rel == "scripts/verify" {
			files[i].Mode = 0o644
		}
	}
	if !hasRule(ValidateSource(files), "template/verify") {
		t.Fatal("a non-executable scripts/verify must be reported")
	}
}

// TestCheckPinnedAcceptsLocalAndDockerReferences pins the exemption a
// template legitimately relies on: a workflow step that runs the
// repository's own action, or a container image, is pinned by the tree
// it lives in, not by a commit SHA.
func TestCheckPinnedAcceptsLocalAndDockerReferences(t *testing.T) {
	for _, uses := range []string{"./.github/actions/local", "docker://alpine:3.20"} {
		var msgs []string
		checkPinned(uses, func(msg string) { msgs = append(msgs, msg) })
		if len(msgs) != 0 {
			t.Errorf("%q must be exempt from SHA pinning, got %v", uses, msgs)
		}
	}
	for _, uses := range []string{"actions/checkout", "actions/checkout@v4", "actions/checkout@abc123"} {
		var msgs []string
		checkPinned(uses, func(msg string) { msgs = append(msgs, msg) })
		if len(msgs) == 0 {
			t.Errorf("%q must be reported as unpinned", uses)
		}
	}
}

// TestBuildReportsAPublishFailure pins that a failure inside the
// staging step surfaces as a build error naming the problem, rather
// than a partially published tree: the destination is checked, and only
// then is the tree assembled beside it.
func TestBuildReportsAPublishFailure(t *testing.T) {
	dir := t.TempDir()
	// A file where the output directory's parent must be, so the
	// staging directory cannot be created beside the destination.
	afile := filepath.Join(dir, "afile")
	if err := os.WriteFile(afile, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(afile, "out")
	_, err := Build(loadFixture(t), "", out, nil, BuildOptions{})
	if err == nil {
		t.Fatal("a build that cannot stage its output must fail")
	}
	if !strings.Contains(err.Error(), "inspecting output directory") {
		t.Errorf("the error must name the refused destination, got %v", err)
	}
	if data, readErr := os.ReadFile(afile); readErr != nil || string(data) != "x\n" {
		t.Errorf("the failed build modified the file: %q (%v)", data, readErr)
	}
}

// TestBuildCreatesTheOutputParent pins the staging path that a plain
// build cannot reach: a destination whose parent does not exist yet is
// created, because the release pipeline builds into a nested path.
func TestBuildCreatesTheOutputParent(t *testing.T) {
	canonical, err := LoadSource(filepath.Join("..", "..", "template"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "nested", "deeper", "out")
	if _, err := Build(canonical, filepath.Join("..", "..", "template"), out, nil, BuildOptions{}); err != nil {
		t.Fatalf("build = %v, want 0", err)
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Fatalf("the built tree must exist under the created parent: %v", err)
	}
}

// TestBuildRefusesAFileAsTheOutputPath pins that a path resolving to
// something that is not a directory is refused by name, with the
// escape hatch named, instead of being overwritten as a file.
func TestBuildRefusesAFileAsTheOutputPath(t *testing.T) {
	dir := t.TempDir()
	afile := filepath.Join(dir, "afile")
	if err := os.WriteFile(afile, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The destination is the file itself: an existing non-directory,
	// which is the case the "exists and is not a directory" refusal
	// exists for. A path *under* the file instead surfaces as an
	// uninspectable parent, covered by the next test.
	_, err := Build(loadFixture(t), "", afile, nil, BuildOptions{})
	var unsafe *UnsafeOutputError
	if !errors.As(err, &unsafe) || !strings.Contains(unsafe.Reason, "not a directory") {
		t.Fatalf("a file at the output path must be refused as unsafe, got %v", err)
	}
	if data, err := os.ReadFile(afile); err != nil || string(data) != "keep me\n" {
		t.Errorf("the refused build modified the file: %q (%v)", data, err)
	}
}

// TestBuildRefusesAnUninspectableOutputPath pins the other
// checkDestination branch: a path whose parent is a file cannot be
// inspected, and that is an error rather than a silent fall-through to
// creating it.
func TestBuildRefusesAnUninspectableOutputPath(t *testing.T) {
	dir := t.TempDir()
	afile := filepath.Join(dir, "afile")
	if err := os.WriteFile(afile, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(loadFixture(t), "", filepath.Join(afile, "out"), nil, BuildOptions{}); err == nil {
		t.Fatal("an output path under a file must be refused")
	}
	if data, err := os.ReadFile(afile); err != nil || string(data) != "x\n" {
		t.Errorf("the refused build modified the file: %q (%v)", data, err)
	}
}

// TestBuildCreatesTheMissingOutputParents pins that --out into a
// directory that does not exist yet is supported: the parents are
// created, so a caller never has to pre-create the tree by hand.
func TestBuildCreatesTheMissingOutputParents(t *testing.T) {
	out := filepath.Join(t.TempDir(), "deeply", "nested", "out")
	if _, err := Build(loadFixture(t), "", out, nil, BuildOptions{}); err != nil {
		t.Fatalf("Build into a not-yet-existing directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Errorf("the build did not produce output under the created parents: %v", err)
	}
}

// TestBuildAbortsWhenTheStagedTreeFailsValidation pins the second
// validation gate: the staged tree is validated again once the manifest
// is real, and a tree that only becomes invalid after rendering aborts
// the publish with the destination untouched.
//
// A Description value is the trigger. The value itself is legal (it is
// validated up front), but it injects a placeholder into the rendered
// output that no known variable resolves, and the staged tree is where
// that is caught — before anything replaces the destination.
func TestBuildAbortsWhenTheStagedTreeFailsValidation(t *testing.T) {
	files := loadFixture(t)
	out := filepath.Join(t.TempDir(), "out")
	// The value carries a placeholder, so the rendered CHANGELOG
	// (which has none of its own) gains one and fails validation.
	vars := map[string]string{"Description": ph("Owner")}
	_, err := Build(files, "", out, vars, BuildOptions{})
	if err == nil {
		t.Fatal("a rendered tree that fails validation must abort the publish")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("an aborted build must leave the destination absent, got %v", statErr)
	}
}

// TestWriteTreeRefusesAFileWhereADirectoryBelongs pins that a source
// list that contradicts itself fails the write rather than producing a
// half-written tree: the nested file is never created, so nothing
// partial is left behind for the swap to publish.
func TestWriteTreeRefusesAFileWhereADirectoryBelongs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a"), []byte("file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeTree(root, []SourceFile{{Rel: "a/b", Mode: 0o600, Data: []byte("nested\n")}})
	if err == nil {
		t.Fatal("a file blocking a directory path must fail the write")
	}
	// The nested path cannot exist: its parent is a regular file.
	if _, err := os.Stat(filepath.Join(root, "a", "b")); err == nil {
		t.Error("the failed write must not leave the nested file behind")
	}
}

// TestSwapIntoPlaceRestoresThePreviousOutput pins the rollback half of
// the publication contract: when the staged tree cannot be moved into
// place, the previous output is moved back, so a failed publish is not
// a lost one, and no backup litter is left in the parent.
func TestSwapIntoPlaceRestoresThePreviousOutput(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "keep.md"), []byte("previous\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A stage that does not exist makes the publish fail after the
	// previous output has been moved aside.
	if err := swapIntoPlace(dest, filepath.Join(parent, "absent-stage")); err == nil {
		t.Fatal("a publish that cannot move the staged tree into place must fail")
	}
	data, err := os.ReadFile(filepath.Join(dest, "keep.md"))
	if err != nil || string(data) != "previous\n" {
		t.Errorf("the previous output was not restored: %q (%v)", data, err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "out" {
		t.Errorf("the rollback left litter in the parent: %+v", entries)
	}
}

// TestManifestOptionsOfRestoresProvenanceFromNil pins that a build with
// no recorded provenance still publishes, rather than dereferencing a
// nil manifest.
func TestManifestOptionsOfRestoresProvenanceFromNil(t *testing.T) {
	if got := manifestOptionsOf(nil); got != (ManifestOptions{}) {
		t.Errorf("a nil manifest must yield the zero provenance, got %+v", got)
	}
}

// TestAdoptManifestLeavesAMalformedManifestAlone pins that a manifest
// the adoption cannot parse is left untouched for validation to report,
// rather than being silently repaired into something that looks valid.
func TestAdoptManifestLeavesAMalformedManifestAlone(t *testing.T) {
	broken := []byte("{not a manifest")
	files := []SourceFile{{Rel: ManifestFileName, Mode: 0o644, Data: broken}}
	out, report := adoptManifest(files)
	if report != nil {
		t.Errorf("an unparseable manifest must not be reported as adopted: %+v", report)
	}
	if len(out) != 1 || string(out[0].Data) != string(broken) {
		t.Errorf("an unparseable manifest must be left byte-identical, got %q", out[0].Data)
	}
}

// TestAdoptSkipsBinaryFiles proves a binary file in the tree is copied
// verbatim: rendering text into a blob would corrupt it, and a binary
// file can never legitimately carry a placeholder.
func TestAdoptSkipsBinaryFiles(t *testing.T) {
	blob := []byte{0x00, 0x01, 0x02, 'P', 'N', 'G', 0x00}
	files := []SourceFile{{Rel: "logo.png", Mode: 0o644, Data: blob}}
	rendered, report, err := Adopt(files, allVars())
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(report) != 0 {
		t.Errorf("a binary file must not be reported as substituted: %+v", report)
	}
	if !bytes.Equal(rendered[0].Data, blob) {
		t.Errorf("a binary file must be copied verbatim, got %q", rendered[0].Data)
	}
}

// TestLoadSourceRefusesAnEmptyTree pins that a source directory with no
// files is an error, not an empty build: publishing an empty tree would
// produce a generated repository with nothing in it.
func TestLoadSourceRefusesAnEmptyTree(t *testing.T) {
	if _, err := LoadSource(t.TempDir()); err == nil {
		t.Error("an empty source tree must be refused")
	}
}

// TestValidateSourceSkipsBinaryFiles pins that a file with no text
// content is never content-checked: the leak and credential rules are
// about what a reader would see, and a NUL byte means there is no text
// to read.
func TestValidateSourceSkipsBinaryFiles(t *testing.T) {
	// A file carrying credential-shaped text but no NUL byte would be
	// reported; with the NUL byte it is binary and skipped.
	secret := "ghp_" + strings.Repeat("a1", 18)
	files := append(loadFixture(t), SourceFile{
		Rel: "logo.png", Mode: 0o644, Data: []byte{0x89, 'P', 'N', 'G', 0x00, 'x'},
	})
	for i := range files {
		if files[i].Rel == "README.md" {
			files[i].Data = []byte("# clean\n")
		}
	}
	if hasRule(ValidateSource(files), "template/secret") {
		t.Error("a binary file must not be content-checked")
	}
	// The same text without the NUL byte is a text file and is caught.
	files = append(loadFixture(t), SourceFile{
		Rel: "notes.txt", Mode: 0o644, Data: []byte(secret + "\n"),
	})
	if !hasRule(ValidateSource(files), "template/secret") {
		t.Error("credential-shaped text must be reported")
	}
}

// TestBuildAcceptsAnEmptyExistingDestination pins that a destination
// which exists but is empty is a valid build target: the directory is
// the caller's own, and replacing it destroys nothing.
func TestBuildAcceptsAnEmptyExistingDestination(t *testing.T) {
	out := t.TempDir()
	if _, err := Build(loadFixture(t), "", out, nil, BuildOptions{}); err != nil {
		t.Fatalf("Build into an existing empty directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Errorf("the build did not produce output: %v", err)
	}
}

// TestPublishTreeCreatesTheMissingOutputParent pins that a destination
// several levels below a directory that does not exist yet is still
// publishable: the parents are created as part of the publish, so a
// caller never has to pre-create the tree by hand.
func TestPublishTreeCreatesTheMissingOutputParent(t *testing.T) {
	dir := t.TempDir()
	// A tree built and read back, minus its manifest, so the staged
	// tree passes the built-tree rules without a real manifest record.
	built := filepath.Join(dir, "built")
	if _, err := Build(loadFixture(t), "", built, nil, BuildOptions{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	var files []SourceFile
	for _, f := range mustLoadSource(t, built) {
		if f.Rel != ManifestFileName {
			files = append(files, f)
		}
	}

	deep := filepath.Join(dir, "missing", "deeper", "out")
	if err := publishTree(deep, files, nil); err != nil {
		t.Fatalf("publishTree into a not-yet-existing parent: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(deep, "README.md")); err != nil || len(data) == 0 {
		t.Errorf("the tree was not published under the created parents: %q (%v)", data, err)
	}
}

func mustLoadSource(t *testing.T, dir string) []SourceFile {
	t.Helper()
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestPublishTreeReportsAnUnstageableParent pins that a parent which
// exists but is a file cannot host the staging directory, and that this
// is reported rather than attempted: nothing is written, because the
// stage is where a tree is assembled before it replaces anything.
func TestPublishTreeReportsAnUnstageableParent(t *testing.T) {
	dir := t.TempDir()
	afile := filepath.Join(dir, "afile")
	if err := os.WriteFile(afile, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The destination's parent is the file itself, so the staging
	// directory cannot be created beside the destination.
	err := publishTree(filepath.Join(afile, "out"),
		[]SourceFile{{Rel: "README.md", Mode: 0o644, Data: []byte("# x\n")}}, nil)
	if err == nil {
		t.Fatal("a parent that is a file must be reported")
	}
	if !strings.Contains(err.Error(), "staging build") {
		t.Errorf("the error must name the staging failure, got %v", err)
	}
	if data, readErr := os.ReadFile(afile); readErr != nil || string(data) != "x\n" {
		t.Errorf("the failed publish modified the file: %q (%v)", data, readErr)
	}
}

// TestSwapIntoPlaceReportsAnUninspectableDestination pins that a
// destination whose parent is a file is an error rather than a silent
// rename target: the previous output cannot be moved aside, so nothing
// is destroyed and the failure is reported.
func TestSwapIntoPlaceReportsAnUninspectableDestination(t *testing.T) {
	dir := t.TempDir()
	afile := filepath.Join(dir, "afile")
	if err := os.WriteFile(afile, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(dir, "stage")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	// Lstat on a path under a file is neither a success nor a
	// not-exist, so the swap must report it rather than treat the
	// destination as absent.
	if err := swapIntoPlace(filepath.Join(afile, "out"), stage); err == nil {
		t.Fatal("an uninspectable destination must be reported")
	}
	if _, err := os.Stat(stage); err != nil {
		t.Errorf("the stage must be left in place when the swap is refused: %v", err)
	}
}

// TestAdoptableTargetRefusesANonRegularPolicy pins that the generated
// policy must be a regular file: a directory at that path is not a
// policy, and adopting over it would rewrite something the project
// does not own.
func TestAdoptableTargetRefusesANonRegularPolicy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".repoforge", "config.yml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AdoptableTarget(dir); err == nil {
		t.Fatal("a directory at .repoforge/config.yml must be refused")
	}
}

// TestInsideEngineTreeRequiresTheFullEngineLayout pins the guard's
// shape: go.mod alone does not make a directory the engine checkout —
// a generated Go project carries one too — so only the full engine
// layout is refused.
func TestInsideEngineTreeRequiresTheFullEngineLayout(t *testing.T) {
	partial := t.TempDir()
	if err := os.WriteFile(filepath.Join(partial, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if insideEngineTree(partial) {
		t.Error("go.mod alone must not be mistaken for the engine checkout")
	}
}
