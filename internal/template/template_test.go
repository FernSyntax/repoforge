package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, dir string) {
	t.Helper()
	// Placeholders are built at runtime so this source file does not
	// trip the repository's own placeholder detector.
	ph := func(name string) string { return "{{" + " ." + name + " }}" }
	files := map[string]string{
		"README.md":                        "# " + ph("ProjectName") + "\n\n" + ph("Description") + "\n",
		"LICENSE":                          "Copyright " + ph("Owner") + "\n",
		"CHANGELOG.md":                     "# Changelog\n",
		"CODE_OF_CONDUCT.md":               "# CoC\n",
		"CONTRIBUTING.md":                  "# Contributing\n",
		"SECURITY.md":                      "# Security\n",
		"SUPPORT.md":                       "# Support\n",
		"AGENTS.md":                        "# Agents\n",
		"DESIGN.md":                        "# Design\n",
		".editorconfig":                    "root = true\n",
		".gitattributes":                   "* text=auto eol=lf\n",
		".gitignore":                       "/dist/\n",
		"scripts/verify":                   "#!/bin/sh\nexit 0\n",
		".github/CODEOWNERS":               "* " + ph("Owner") + "\n",
		".github/PULL_REQUEST_TEMPLATE.md": "## What changed\n",
		".github/dependabot.yml":           "version: 2\nupdates: []\n",
		".github/workflows/ci.yml":         "name: CI\non: [push]\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1\n",
		".repoforge/config.yml":            "standard:\n  version: \"" + ph("StandardVersion") + "\"\n",
		".repoforge/ownership.yml":         "managed_paths:\n  - \".github/workflows/*.yml\"\n",
		"spec.md":                          "# Project Specification\n\n<<PROJECT NAME>>\n",
		"docs/decisions/README.md":         "# Decisions\n\n| ADR | Title | Status |\n",
		"docs/decisions/0000-template.md":  "# ADR 0000: template\n",
		"docs/wiki/Home.md":                "# Home\n",
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
}

func loadFixture(t *testing.T) []SourceFile {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir)
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func hasRule(issues []Issue, rule string) bool {
	for _, i := range issues {
		if i.Rule == rule {
			return true
		}
	}
	return false
}

func TestValidateSourceOK(t *testing.T) {
	files := loadFixture(t)
	if issues := ValidateSource(files); len(issues) > 0 {
		t.Fatalf("expected clean source, got: %+v", issues)
	}
}

func TestValidateMissingRequired(t *testing.T) {
	files := loadFixture(t)
	var kept []SourceFile
	for _, f := range files {
		if f.Rel != "LICENSE" {
			kept = append(kept, f)
		}
	}
	issues := ValidateSource(kept)
	if !hasRule(issues, "template/required") {
		t.Fatalf("expected template/required issue, got: %+v", issues)
	}
}

func TestValidateUnknownPlaceholder(t *testing.T) {
	files := loadFixture(t)
	files[0].Data = append(files[0].Data, []byte("{{"+" .Bogus }}\n")...)
	issues := ValidateSource(files)
	if !hasRule(issues, "template/placeholder") {
		t.Fatalf("expected template/placeholder issue, got: %+v", issues)
	}
}

func TestValidateEngineLeak(t *testing.T) {
	files := loadFixture(t)
	files = append(files, SourceFile{Rel: "main.go", Data: []byte("package main\n")})
	files = append(files, SourceFile{Rel: "go.mod", Data: []byte("module x\n")})
	issues := ValidateSource(files)
	if !hasRule(issues, "template/leak") {
		t.Fatalf("expected template/leak issue, got: %+v", issues)
	}
}

func TestValidateSecret(t *testing.T) {
	files := loadFixture(t)
	token := "ghp_" + strings.Repeat("a1", 18)
	files[0].Data = append(files[0].Data, []byte(token+"\n")...)
	issues := ValidateSource(files)
	if !hasRule(issues, "template/secret") {
		t.Fatalf("expected template/secret issue, got: %+v", issues)
	}
}

func TestValidateIdentity(t *testing.T) {
	files := loadFixture(t)
	// Built at runtime so this source file never carries a literal
	// address itself.
	mail := "maintainer" + "@" + "example.com"
	files[0].Data = append(files[0].Data, []byte("contact: "+mail+"\n")...)
	issues := ValidateSource(files)
	if !hasRule(issues, "template/identity") {
		t.Fatalf("expected template/identity issue, got: %+v", issues)
	}
}

func TestValidatePlaceholderOwnerIsNotIdentity(t *testing.T) {
	// Ownership placeholders are the sanctioned mechanism (spec.md §13.4), not leaks: the clean fixture must stay identity-clean.
	issues := ValidateSource(loadFixture(t))
	if hasRule(issues, "template/identity") {
		t.Fatalf("placeholders must not trip identity: %+v", issues)
	}
}

func TestValidateUnpinnedAction(t *testing.T) {
	files := loadFixture(t)
	for i := range files {
		if strings.HasSuffix(files[i].Rel, "workflows/ci.yml") {
			files[i].Data = []byte("name: CI\non: [push]\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v7\n")
		}
	}
	issues := ValidateSource(files)
	if !hasRule(issues, "template/pinning") {
		t.Fatalf("expected template/pinning issue, got: %+v", issues)
	}
}

func allVars() map[string]string {
	return map[string]string{
		"ProjectName": "demo", "Owner": "octo",
		"RepoURL":     "https://example.invalid/o/demo",
		"Description": "Demo project", "StandardVersion": "v1",
	}
}

func TestAdoptRendersAll(t *testing.T) {
	files := loadFixture(t)
	before := string(files[0].Data)
	rendered, report, err := Adopt(files, allVars())
	if err != nil {
		t.Fatal(err)
	}
	if len(report) == 0 {
		t.Fatal("expected substitutions, got empty report")
	}
	if string(files[0].Data) != before {
		t.Error("Adopt modified its input slice")
	}
	// No known placeholder may survive a complete adoption.
	for _, f := range rendered {
		if isBinary(f.Data) {
			continue
		}
		for _, m := range placeholderRe.FindAllStringSubmatch(string(f.Data), -1) {
			t.Errorf("%s: placeholder {{ .%s }} survived adoption", f.Rel, m[1])
		}
	}
}

func TestAdoptMissingVarFails(t *testing.T) {
	vars := allVars()
	delete(vars, "Owner")
	if _, _, err := Adopt(loadFixture(t), vars); err == nil {
		t.Error("missing variable must fail, not partially adopt")
	}
}

func TestAdoptUnknownVarFails(t *testing.T) {
	vars := allVars()
	vars["Evil"] = "x"
	if _, _, err := Adopt(loadFixture(t), vars); err == nil {
		t.Error("unknown variable must fail")
	}
}

func TestAdoptIdempotent(t *testing.T) {
	rendered, _, err := Adopt(loadFixture(t), allVars())
	if err != nil {
		t.Fatal(err)
	}
	_, report, err := Adopt(rendered, allVars())
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 0 {
		t.Errorf("second adoption must be a no-op, got %+v", report)
	}
}

func TestAdoptSkipsBinary(t *testing.T) {
	files := []SourceFile{{Rel: "img.png", Data: []byte{0x89, 'P', 'N', 'G'}}}
	rendered, report, err := Adopt(files, allVars())
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 0 || len(rendered) != 1 {
		t.Errorf("binary must pass through untouched: %+v", report)
	}
}

func TestAdoptableTarget(t *testing.T) {
	if err := AdoptableTarget(t.TempDir()); err == nil {
		t.Error("directory without generated policy must be refused")
	}
	mk := func(root string, names ...string) {
		t.Helper()
		for _, n := range names {
			p := filepath.Join(root, n)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A generated Go project (go.mod, no engine layout) stays adoptable.
	proj := t.TempDir()
	mk(proj, ".repoforge/config.yml", "go.mod")
	if err := AdoptableTarget(proj); err != nil {
		t.Errorf("generated project must be adoptable: %v", err)
	}
	// Anything nested under the engine layout is refused, including
	// relative paths into it.
	engine := t.TempDir()
	mk(engine, "go.mod", "internal/template/engine.go")
	nested := filepath.Join(engine, "template")
	mk(nested, ".repoforge/config.yml")
	if err := AdoptableTarget(nested); err == nil {
		t.Error("directory nested under the engine layout must be refused")
	}
}

func TestBuildRendersAndLeavesIntentionalPlaceholders(t *testing.T) {
	files := loadFixture(t)
	out := t.TempDir() + "/out"
	issues, err := Build(files, "", out, map[string]string{
		"ProjectName":     "demo",
		"Description":     "A demo project",
		"StandardVersion": "v1",
	}, BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v (issues: %+v)", err, issues)
	}
	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "# demo") {
		t.Errorf("README not rendered: %q", readme)
	}
	codeowners, _ := os.ReadFile(filepath.Join(out, ".github/CODEOWNERS"))
	if !strings.Contains(string(codeowners), "{{"+" .Owner }}") {
		t.Errorf("unprovided variable should remain as intentional placeholder: %q", codeowners)
	}
}

func TestBuildDeterministic(t *testing.T) {
	files := loadFixture(t)
	vars := map[string]string{"ProjectName": "demo", "Description": "d", "StandardVersion": "v1"}
	tree1 := buildTree(t, files, t.TempDir()+"/out", vars)
	tree2 := buildTree(t, files, t.TempDir()+"/out", vars)
	if len(tree1) != len(tree2) {
		t.Fatalf("tree sizes differ: %d vs %d", len(tree1), len(tree2))
	}
	for k, v1 := range tree1 {
		if tree2[k] != v1 {
			t.Errorf("content differs for %s", k)
		}
	}
}

func buildTree(t *testing.T, files []SourceFile, out string, vars map[string]string) map[string]string {
	t.Helper()
	if _, err := Build(files, "", out, vars, BuildOptions{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	tree := map[string]string{}
	root := out
	err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		data, _ := os.ReadFile(path)
		tree[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// TestBuildRefusesNonEmptyDestination pins the overwrite contract: a
// non-empty destination is never deleted silently, whatever it
// contains. --force opts in.
func TestBuildRefusesNonEmptyDestination(t *testing.T) {
	files := loadFixture(t)
	out := t.TempDir() + "/out"
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "stale.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(files, "", out, map[string]string{"StandardVersion": "v1"}, BuildOptions{}); err == nil {
		t.Fatal("non-empty destination must be refused without --force")
	}
	if data, err := os.ReadFile(filepath.Join(out, "stale.txt")); err != nil || string(data) != "junk" {
		t.Errorf("refused build modified the destination: %q (%v)", data, err)
	}
}

// TestBuildForceReplacesNonEmptyDestination pins the other half: an
// explicit request replaces the whole destination, and does so through
// a staged tree so a reader never sees a partial result.
func TestBuildForceReplacesNonEmptyDestination(t *testing.T) {
	files := loadFixture(t)
	out := t.TempDir() + "/out"
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "stale.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(files, "", out, map[string]string{"StandardVersion": "v1"}, BuildOptions{Force: true}); err != nil {
		t.Fatalf("Build with force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "stale.txt")); !os.IsNotExist(err) {
		t.Error("stale file survived a forced build")
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Errorf("forced build did not produce output: %v", err)
	}
}

// TestBuildPreservesExecutableBit guards the file-mode contract
// (scripts/verify must stay runnable in the generated repository).
func TestBuildPreservesExecutableBit(t *testing.T) {
	files := loadFixture(t)
	out := t.TempDir() + "/out"
	if _, err := Build(files, "", out, map[string]string{"StandardVersion": "v1"}, BuildOptions{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	info, err := os.Stat(filepath.Join(out, "scripts", "verify"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("scripts/verify lost its executable bit: %v", info.Mode())
	}
}

// TestRealTemplateSourceValid guards the shipped template/ tree against
// drift: it must always pass its own validation.
func TestRealTemplateSourceValid(t *testing.T) {
	files, err := LoadSource("../../template")
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if issues := ValidateSource(files); len(issues) > 0 {
		for _, i := range issues {
			t.Errorf("%s", i)
		}
	}
}
