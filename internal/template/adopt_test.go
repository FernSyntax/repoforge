package template

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// generatedProject builds a realistic generated repository: the model
// baseline plus the Go project files a real consumer would have. It
// carries several intentional placeholders.
func generatedProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Placeholders are built at runtime by the package-level ph helper
	// (placeholder_test.go) so this file carries no literal placeholder syntax.
	files := map[string]string{
		"README.md":                        "# " + ph("ProjectName") + "\n\n" + ph("Description") + "\n\n" + ph("RepoURL") + "\n",
		"LICENSE":                          "Copyright " + ph("Owner") + "\n",
		"CHANGELOG.md":                     "# Changelog\n",
		"CODE_OF_CONDUCT.md":               "# CoC\n",
		"CONTRIBUTING.md":                  "# Contributing\n",
		"SECURITY.md":                      "# Security\n",
		"SUPPORT.md":                       "# Support\n",
		"AGENTS.md":                        "# Agents\n",
		".editorconfig":                    "root = true\n",
		".gitattributes":                   "* text=auto eol=lf\n",
		".gitignore":                       "/dist/\n",
		"scripts/verify":                   "#!/bin/sh\nexit 0\n",
		".github/CODEOWNERS":               "* " + ph("Owner") + "\n/.github/workflows/ " + ph("Owner") + "\n",
		".github/PULL_REQUEST_TEMPLATE.md": "## What changed\n",
		".github/dependabot.yml":           "version: 2\nupdates: []\n",
		".github/workflows/ci.yml":         "name: CI\non: [push]\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1\n",
		// The consumer's own Go project: legitimate content that
		// template-source validation would wrongly call engine leakage.
		"go.mod":              "module example.com/demo\n\ngo 1.27\n",
		"go.sum":              "\n",
		"main.go":             "package main\n\nfunc main() { println(\"" + ph("ProjectName") + "\") }\n",
		"internal/app/app.go": "package app\n\n// Name is " + ph("ProjectName") + ".\nconst Name = \"" + ph("ProjectName") + "\"\n",
		// The generated policy documents.
		".repoforge/config.yml":    "# Config for " + ph("ProjectName") + "\nstandard:\n  name: repoforge\n  version: \"" + ph("StandardVersion") + "\"\nrepository:\n  governance_profile: solo\ngithub:\n  issues: true\n  wiki: true\n",
		".repoforge/ownership.yml": "# Ownership for " + ph("ProjectName") + "\nmanaged_paths:\n  - \".github/workflows/*.yml\"\n",
		// A consumer file that no placeholder touches.
		"docs/notes.md": "Consumer notes.\n",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if name == "scripts/verify" {
			mode = 0o755
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func adoptionVars() map[string]string {
	return map[string]string{
		"ProjectName":     "demo",
		"Owner":           "acme",
		"RepoURL":         "https://github.com/acme/demo",
		"Description":     "A demo project",
		"StandardVersion": "v1",
	}
}

// TestAdoptGeneratedGoProject is the documented-user-journey test: a
// real generated repository carrying .go files and go.mod adopts
// cleanly, and the rendered Go/YAML content is valid, not corrupted.
func TestAdoptGeneratedGoProject(t *testing.T) {
	dir := generatedProject(t)
	if err := AdoptableTarget(dir); err != nil {
		t.Fatalf("generated Go project must be adoptable: %v", err)
	}
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	rendered, report, err := Adopt(files, adoptionVars())
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(report) == 0 {
		t.Fatal("no substitutions reported")
	}
	if issues := ValidateAdoption(ChangedFiles(files, rendered)); len(issues) > 0 {
		t.Fatalf("rendered repo failed the generated-repository contract: %+v", issues)
	}

	// No known placeholder may survive anywhere.
	for _, f := range rendered {
		if isBinary(f.Data) {
			continue
		}
		if strings.Contains(string(f.Data), "{{"+" .") {
			t.Errorf("%s still carries a placeholder", f.Rel)
		}
	}
	// Rendered content is the consumer's own content, intact.
	for _, f := range rendered {
		switch f.Rel {
		case "go.mod":
			if string(f.Data) != "module example.com/demo\n\ngo 1.27\n" {
				t.Errorf("go.mod altered: %q", f.Data)
			}
		case "internal/app/app.go":
			if !strings.Contains(string(f.Data), `const Name = "demo"`) {
				t.Errorf("app.go not rendered: %q", f.Data)
			}
		case ".repoforge/config.yml":
			if !strings.Contains(string(f.Data), `version: "v1"`) {
				t.Errorf("policy version not rendered: %q", f.Data)
			}
		}
	}

	// Now publish and confirm the same on disk.
	res, err := PublishAdoption(dir, files, rendered)
	if err != nil {
		t.Fatalf("PublishAdoption: %v", err)
	}
	if len(res.Written) == 0 {
		t.Fatal("nothing written")
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, ".repoforge", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), `version: "v1"`) {
		t.Errorf("on-disk policy not rendered: %q", onDisk)
	}
	// Executable bit preserved.
	info, err := os.Stat(filepath.Join(dir, "scripts", "verify"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("scripts/verify lost its executable bit: %v", info.Mode())
	}
	// Consumer files untouched.
	if data, _ := os.ReadFile(filepath.Join(dir, "docs", "notes.md")); string(data) != "Consumer notes.\n" {
		t.Errorf("unrelated file changed: %q", data)
	}
}

// TestAdoptGeneratedContractAcceptsGoProject proves the validation
// boundary: ValidateGenerated must not apply template-source
// engine-leakage rules to a generated repository.
func TestAdoptGeneratedContractAcceptsGoProject(t *testing.T) {
	dir := generatedProject(t)
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The source rules still call these files leaks — that is correct
	// for the canonical template source.
	if !hasRule(ValidateSource(files), "template/leak") {
		t.Error("ValidateSource must still treat engine artifacts as leaks")
	}
	// The generated-repository contract must accept them.
	if issues := ValidateGenerated(files); len(issues) > 0 {
		t.Errorf("ValidateGenerated rejected a legitimate Go project: %+v", issues)
	}
}

// TestAdoptIsIdempotent proves re-running with the same values is a
// no-op: no writes, no diff, no report.
func TestAdoptIsIdempotent(t *testing.T) {
	dir := generatedProject(t)
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, report, err := Adopt(files, adoptionVars())
	if err != nil {
		t.Fatal(err)
	}
	res, err := PublishAdoption(dir, files, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != len(report) {
		t.Errorf("wrote %d files, report says %d", len(res.Written), len(report))
	}

	second, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ChangedFiles(second, first)) != 0 {
		t.Error("re-adoption reports a diff")
	}
	again, report2, err := Adopt(second, adoptionVars())
	if err != nil {
		t.Fatal(err)
	}
	if len(report2) != 0 {
		t.Errorf("second adoption reported %d substitutions", len(report2))
	}
	res2, err := PublishAdoption(dir, second, again)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Written) != 0 {
		t.Errorf("second adoption wrote %d files", len(res2.Written))
	}
}

// TestAdoptDryRunChangesNothing pins the dry-run contract at the layer
// that actually writes: computing the diff and refusing to publish.
func TestAdoptDryRunChangesNothing(t *testing.T) {
	dir := generatedProject(t)
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	rendered, report, err := Adopt(files, adoptionVars())
	if err != nil {
		t.Fatal(err)
	}
	if len(report) == 0 {
		t.Fatal("expected substitutions")
	}
	// The dry run is exactly "compute, then do not publish": every
	// validation runs, and no file is written.
	if issues := ValidateAdoption(ChangedFiles(files, rendered)); len(issues) > 0 {
		t.Fatalf("dry run validation failed: %+v", issues)
	}
	data, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "{{"+" .ProjectName }}") {
		t.Error("dry run wrote to the repository")
	}
}

// TestAdoptRejectsMissingUnknownAndDuplicateVariables pins the closed
// variable set and the completeness rule.
func TestAdoptRejectsMissingUnknownAndDuplicateVariables(t *testing.T) {
	files := loadFixture(t)

	if _, _, err := Adopt(files, map[string]string{
		"ProjectName": "demo", "Owner": "acme", "RepoURL": "https://x.invalid/a", "Description": "d",
	}); err == nil {
		t.Error("missing a required variable must fail")
	}
	if _, _, err := Adopt(files, adoptionVarsWith("Bogus", "x")); err == nil {
		t.Error("unknown variable must fail")
	}
	for _, bad := range []struct{ name, value string }{
		{"Owner", ""},
		{"Owner", "bad owner"},
		{"ProjectName", "bad/name"},
		{"Description", "line\nbreak"},
		{"RepoURL", "nope"},
		{"StandardVersion", "9"},
		{"Description", ph("Owner")},
	} {
		if _, _, err := Adopt(files, adoptionVarsWith(bad.name, bad.value)); err == nil {
			t.Errorf("%s=%q must be rejected", bad.name, bad.value)
		}
	}
}

func adoptionVarsWith(name, value string) map[string]string {
	v := adoptionVars()
	v[name] = value
	return v
}

// TestAdoptDetectsUnresolvedPlaceholders proves a render that leaves a
// known placeholder behind is caught before writing.
func TestAdoptDetectsUnresolvedPlaceholders(t *testing.T) {
	// A file the varFlags layer could never reach, plus a valid render
	// everywhere else: any survivor must be reported.
	files := []SourceFile{
		{Rel: "README.md", Data: []byte("# demo\n"), Mode: 0o644},
		{Rel: "docs/leftover.md", Data: []byte("owner: " + ph("Owner")), Mode: 0o644},
	}
	issues := CheckNoUnresolvedPlaceholders(files)
	if !hasRule(issues, "generated/placeholder") {
		t.Fatalf("expected generated/placeholder issue, got %+v", issues)
	}
	if len(ValidateAdoption(files)) == 0 {
		t.Error("ValidateAdoption must surface unresolved placeholders")
	}
	// Unknown placeholders are the template-source checker's business,
	// not adoption's.
	if got := CheckNoUnresolvedPlaceholders([]SourceFile{{Rel: "a", Data: []byte(ph("Bogus")), Mode: 0o644}}); len(got) != 0 {
		t.Errorf("unknown placeholder must not be an adoption issue: %+v", got)
	}
}

// TestAdoptDetectsInvalidYAMLAfterRendering proves a value that would
// corrupt YAML is caught by validation, not written.
func TestAdoptDetectsInvalidYAMLAfterRendering(t *testing.T) {
	files := []SourceFile{
		{Rel: ".repoforge/config.yml", Data: []byte("standard:\n  version: \"v1\"\n  bad: [unclosed\n"), Mode: 0o644},
	}
	if issues := ValidateGenerated(files); !hasRule(issues, "generated/yaml") {
		t.Fatalf("expected generated/yaml issue, got %+v", issues)
	}
}

// TestAdoptRefusesSymlinkTarget proves adoption never writes through a
// symlink placed at a baseline path.
func TestAdoptRefusesSymlinkTarget(t *testing.T) {
	dir := generatedProject(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	// The outside file carries a placeholder, so adoption genuinely
	// wants to write it through the symlink.
	if err := os.WriteFile(outside, []byte("Copyright "+ph("Owner")), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "LICENSE")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before, _ := os.ReadFile(outside)

	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	rendered, _, err := Adopt(files, adoptionVars())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublishAdoption(dir, files, rendered); err == nil {
		t.Fatal("publishing over a symlink must be refused")
	}
	after, _ := os.ReadFile(outside)
	if !bytes.Equal(before, after) {
		t.Errorf("file behind the symlink was modified: %q -> %q", before, after)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink was replaced: %v", err)
	}
}

// TestAdoptIsAtomicOnWriteFailure proves a failure during the write
// phase rolls the repository back to its pre-adoption state.
func TestAdoptIsAtomicOnWriteFailure(t *testing.T) {
	dir := generatedProject(t)
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	rendered, _, err := Adopt(files, adoptionVars())
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, dir)

	// Make one destination unwritable by replacing a file the adoption
	// will touch with a directory; the write loop must fail there and
	// roll back everything published before it.
	changed := ChangedFiles(files, rendered)
	target := ""
	for _, f := range changed {
		if strings.Contains(f.Rel, "/") {
			target = f.Rel
			break
		}
	}
	if target == "" {
		t.Skip("no nested file in the fixture to make fail")
	}
	dest := filepath.Join(dir, filepath.FromSlash(target))
	if err := os.RemoveAll(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err = PublishAdoption(dir, files, rendered)
	if err == nil {
		t.Fatal("expected the write phase to fail")
	}
	// Rolled back: every ordinary file is byte-identical to
	// pre-adoption. The only permitted difference is the directory we
	// substituted in place of the target file, which the rollback
	// cannot turn back into a file because we deleted it.
	after := snapshotTree(t, dir)
	for rel, digest := range before {
		if rel == target {
			continue
		}
		if after[rel] != digest {
			t.Errorf("%s changed after a rolled-back adoption:\n was %s\n now %s", rel, digest, after[rel])
		}
	}
	if _, ok := after[target]; ok {
		t.Errorf("%s should be the directory we substituted, not a written file", target)
	}
}

func changedHash(files []SourceFile, rel string) string {
	for _, f := range files {
		if f.Rel == rel {
			sum := sha256.Sum256(f.Data)
			return hex.EncodeToString(sum[:])
		}
	}
	return ""
}

// TestAdoptRollsBackAndReportsFailure verifies the rollback error path
// is loud rather than silent when it cannot fully recover.
func TestAdoptRollsBackAndReportsFailure(t *testing.T) {
	dir := generatedProject(t)
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	rendered, _, err := Adopt(files, adoptionVars())
	if err != nil {
		t.Fatal(err)
	}
	changed := ChangedFiles(files, rendered)
	var first string
	for _, f := range changed {
		first = f.Rel
		break
	}
	// Directly failing on the very first file leaves nothing to roll
	// back, and the error must still name the file.
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(first))); err != nil {
		t.Fatal(err)
	}
	_, err = PublishAdoption(dir, files, rendered)
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), first) {
		t.Errorf("error does not name the failing file: %v", err)
	}
}

// TestAdoptRejectsEngineCheckout pins the engine guard, including
// symlinked and relative paths into it.
func TestAdoptRejectsEngineCheckout(t *testing.T) {
	plain := t.TempDir()
	if err := AdoptableTarget(plain); err == nil {
		t.Error("directory without a generated policy must be refused")
	}

	engine := t.TempDir()
	for _, n := range []string{"go.mod", "template/README.md"} {
		p := filepath.Join(engine, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(engine, "internal", "template"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(engine, "internal", "template", "t.go"), []byte("package template"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(engine, "template")
	if err := os.MkdirAll(filepath.Join(nested, ".repoforge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".repoforge", "config.yml"), []byte("standard:\n  version: v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AdoptableTarget(nested); err == nil {
		t.Error("engine checkout must be refused")
	}

	// A symlink into the engine must be refused too.
	link := filepath.Join(t.TempDir(), "sneaky")
	if err := os.Symlink(engine, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := AdoptableTarget(link); err == nil {
		t.Error("symlink into the engine checkout must be refused")
	}

	// An empty target is refused.
	if err := AdoptableTarget(""); err == nil {
		t.Error("empty target must be refused")
	}
}

// TestAdoptPreservesUserModifications proves adoption only rewrites
// files whose rendered bytes differ, so a user edit elsewhere survives.
func TestAdoptPreservesUserModifications(t *testing.T) {
	dir := generatedProject(t)
	userFile := filepath.Join(dir, "docs", "notes.md")
	if err := os.WriteFile(userFile, []byte("Consumer wrote this.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A dirty tree: the adoption target itself has an uncommitted edit.
	readme := filepath.Join(dir, "README.md")
	data, _ := os.ReadFile(readme)
	if err := os.WriteFile(readme, append(data, []byte("\nLocal note\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	rendered, _, err := Adopt(files, adoptionVars())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublishAdoption(dir, files, rendered); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(userFile); string(data) != "Consumer wrote this.\n" {
		t.Errorf("user file changed: %q", data)
	}
	if data, _ := os.ReadFile(readme); !strings.Contains(string(data), "Local note") {
		t.Errorf("user modification inside a rendered file was lost: %q", data)
	}
}

// TestPublishAdoptionRejectsPathEscape proves a malicious relative path
// in a source file cannot escape the target directory.
func TestPublishAdoptionRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	before := []SourceFile{{Rel: "a", Data: []byte("a"), Mode: 0o644}}
	after := []SourceFile{
		{Rel: "a", Data: []byte("a"), Mode: 0o644},
		{Rel: "../escape", Data: []byte("pwned"), Mode: 0o644},
	}
	if _, err := PublishAdoption(dir, before, after); err == nil {
		t.Fatal("path escaping the target directory must be refused")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); !os.IsNotExist(err) {
		t.Error("file was written outside the target directory")
	}
}

// TestAdoptReportDeterminism proves the report is stable in content and
// order across runs, so it can be diffed and scripted.
func TestAdoptReportDeterminism(t *testing.T) {
	files := loadFixture(t)
	vars := map[string]string{
		"ProjectName": "demo", "Owner": "acme", "RepoURL": "https://x.invalid/a",
		"Description": "d", "StandardVersion": "v1",
	}
	var first []AdoptReport
	for i := 0; i < 50; i++ {
		_, report, err := Adopt(files, vars)
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = report
			continue
		}
		if len(report) != len(first) {
			t.Fatalf("report length varies: %d vs %d", len(report), len(first))
		}
		for j := range report {
			if report[j].Rel != first[j].Rel {
				t.Fatalf("report order varies at %d: %s vs %s", j, report[j].Rel, first[j].Rel)
			}
			if Describe(report[j].Substitutions) != Describe(first[j].Substitutions) {
				t.Fatalf("substitution summary varies for %s", report[j].Rel)
			}
		}
	}
}

// TestAdoptEmptyIsNoop pins the empty-diff contract.
func TestAdoptEmptyIsNoop(t *testing.T) {
	dir := t.TempDir()
	base := []SourceFile{{Rel: "a", Data: []byte("same"), Mode: 0o644}}
	res, err := PublishAdoption(dir, base, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != 0 {
		t.Errorf("wrote %d files for an empty diff", len(res.Written))
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("target directory damaged: %v", err)
	}
}
