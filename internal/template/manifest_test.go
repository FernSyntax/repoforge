// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func devFiles() []SourceFile {
	return []SourceFile{
		{Rel: "README.md", Mode: 0o644, Data: []byte("# demo\n")},
		{Rel: "scripts/verify", Mode: 0o755, Data: []byte("#!/bin/sh\n")},
	}
}

// writeDevTree materializes devFiles plus a freshly built manifest,
// without going through Build (which enforces the full source rules).
func writeDevTree(t *testing.T, opts ManifestOptions) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := devFiles()
	for _, f := range files {
		dest := filepath.Join(dir, filepath.FromSlash(f.Rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, f.Data, fileMode(f)); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteManifest(dir, BuildManifest(files, opts)); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestManifestDigestDoesNotCoverItself proves the digest is computed
// over the canonical manifest with the digest field blank, so a rebuild
// from the same provenance reproduces the same digest instead of
// chasing its own tail.
func TestManifestDigestDoesNotCoverItself(t *testing.T) {
	files := devFiles()
	opts := ManifestOptions{SourceRef: "v1.2.3", SourceCommit: strings.Repeat("a", 40)}
	first := BuildManifest(files, opts)
	second := BuildManifest(files, opts)
	if first.ManifestDigest == "" {
		t.Fatal("manifest digest is empty")
	}
	if first.ManifestDigest != second.ManifestDigest {
		t.Fatalf("manifest is not deterministic: %s != %s", first.ManifestDigest, second.ManifestDigest)
	}
	// The digest must not depend on its own field: recomputing it after
	// overwriting the recorded value yields the same answer, which is
	// only true when the digest field is blanked before hashing.
	first.ManifestDigest = "not-the-real-digest"
	if got := first.digest(); got != second.ManifestDigest {
		t.Fatalf("digest depends on its own field: %s != %s", got, second.ManifestDigest)
	}
	// Changing provenance must change the digest: it is recorded
	// provenance, not decorative metadata.
	other := BuildManifest(files, ManifestOptions{SourceRef: "v9.9.9", SourceCommit: strings.Repeat("a", 40)})
	if other.ManifestDigest == first.ManifestDigest {
		t.Error("a different source ref produced the same manifest digest")
	}
}

// TestManifestDetectsPostGenerationEdits proves editing the manifest
// fails loudly instead of letting a tampered tree look canonical.
func TestManifestDetectsPostGenerationEdits(t *testing.T) {
	out := writeDevTree(t, ManifestOptions{RepoForgeVersion: "v1.2.3", SourceRef: "v1.2.3", SourceCommit: strings.Repeat("a", 40), SourceURL: "https://example.invalid/repoforge"})

	m, err := LoadManifest(out)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !m.Verifiable() {
		t.Error("a manifest with a source commit and URL must be verifiable")
	}
	m.Files[0].SHA256 = strings.Repeat("0", 64)
	if err := WriteManifest(out, m); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := LoadManifest(out); err == nil {
		t.Fatal("an edited manifest must be rejected")
	}
}

// TestDevelopmentManifestIsNotVerifiable proves an unreleased build is
// valid but explicitly untraceable.
func TestDevelopmentManifestIsNotVerifiable(t *testing.T) {
	m := BuildManifest(devFiles(), ManifestOptions{})
	if err := m.Valid(); err != nil {
		t.Fatalf("a development manifest must still be valid: %v", err)
	}
	if m.Verifiable() {
		t.Error("a manifest without a source commit must not claim to be verifiable")
	}
}

// TestManifestRecordsEveryFileAndMode proves the manifest describes the
// whole tree in sorted order, including permission bits.
func TestManifestRecordsEveryFileAndMode(t *testing.T) {
	files := append(devFiles(), SourceFile{Rel: ".github/workflows/ci.yml", Mode: 0o644, Data: []byte("name: ci\n")})
	m := BuildManifest(files, ManifestOptions{})
	if len(m.Files) != len(files) {
		t.Fatalf("manifest lists %d files, want %d", len(m.Files), len(files))
	}
	wantOrder := []string{".github/workflows/ci.yml", "README.md", "scripts/verify"}
	for i, want := range wantOrder {
		if m.Files[i].Path != want {
			t.Errorf("entry %d is %q, want %q", i, m.Files[i].Path, want)
		}
	}
	if m.Files[2].Mode != "0755" {
		t.Errorf("executable mode recorded as %q, want 0755", m.Files[2].Mode)
	}
	if err := m.Valid(); err != nil {
		t.Errorf("manifest is not valid: %v", err)
	}
}

// TestManifestRejectsSelfEntryAndUnsorted proves the two structural
// rules that keep a manifest comparable.
func TestManifestRejectsSelfEntryAndUnsorted(t *testing.T) {
	m := BuildManifest(devFiles(), ManifestOptions{})
	m.Files = append(m.Files, ManifestFile{Path: ManifestFileName, SHA256: strings.Repeat("1", 64)})
	if err := m.Valid(); err == nil {
		t.Error("a manifest that lists itself must be rejected")
	}

	m = BuildManifest(devFiles(), ManifestOptions{})
	m.Files[0], m.Files[1] = m.Files[1], m.Files[0]
	if err := m.Valid(); err == nil {
		t.Error("an unsorted manifest must be rejected")
	}

	m = BuildManifest(devFiles(), ManifestOptions{SourceCommit: "deadbeef"})
	if err := m.Valid(); err == nil {
		t.Error("a short source commit must be rejected")
	}
}

// TestValidateBuiltTreeRequiresManifest proves a built tree without a
// manifest is an issue.
func TestValidateBuiltTreeRequiresManifest(t *testing.T) {
	issues := ValidateBuiltTree(devFiles())
	if len(issues) == 0 {
		t.Fatal("a built tree without a manifest must be an issue")
	}
	if !hasRule(issues, "manifest/missing") {
		t.Errorf("issues = %v, want manifest/missing", issues)
	}
}

// TestValidateBuiltTreeAcceptsManifestPlusFiles proves the happy path.
func TestValidateBuiltTreeAcceptsManifestPlusFiles(t *testing.T) {
	out := writeDevTree(t, ManifestOptions{})
	files, err := LoadSource(out)
	if err != nil {
		t.Fatal(err)
	}
	if issues := ValidateBuiltTree(files); hasRule(issues, "manifest/missing") || hasRule(issues, "manifest/invalid") {
		t.Errorf("a manifest-bearing tree must not report manifest problems: %v", issues)
	}
}

// TestManifestVerifyCatchesAddedAndChangedFiles proves the comparison
// is symmetric: extra files and altered files are both findings.
func TestManifestVerifyCatchesAddedAndChangedFiles(t *testing.T) {
	files := devFiles()
	m := BuildManifest(files, ManifestOptions{})

	altered := []SourceFile{
		files[0],
		{Rel: "scripts/verify", Mode: 0o755, Data: []byte("#!/bin/sh\necho changed\n")},
	}
	if issues := m.Verify(altered); len(issues) == 0 {
		t.Error("an altered file must be reported")
	}

	extra := append(append([]SourceFile{}, files...), SourceFile{Rel: "EXTRA.md", Mode: 0o644, Data: []byte("x\n")})
	if issues := m.Verify(extra); !hasRule(issues, "manifest/extra") {
		t.Errorf("issues = %v, want manifest/extra", issues)
	}
}

// TestWriteManifestIsStable proves two writes of the same manifest are
// byte-identical, which the publication comparison relies on.
func TestWriteManifestIsStable(t *testing.T) {
	out := writeDevTree(t, ManifestOptions{RepoForgeVersion: "v1.2.3", SourceRef: "v1.2.3", SourceCommit: strings.Repeat("a", 40), SourceURL: "https://example.invalid/repoforge"})
	first, err := os.ReadFile(filepath.Join(out, ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(out, m); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(out, ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("manifest serialization is not stable")
	}
	var probe map[string]any
	if err := json.Unmarshal(first, &probe); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	for _, key := range []string{"schema_version", "repoforge_version", "source_url", "files", "manifest_digest"} {
		if _, ok := probe[key]; !ok {
			t.Errorf("manifest is missing the %q field", key)
		}
	}
}

// TestManifestRejectsUnknownField proves a manifest that gained a
// property the tool does not understand is refused rather than silently
// truncated.
func TestManifestRejectsUnknownField(t *testing.T) {
	dir := writeDevTree(t, ManifestOptions{})
	path := filepath.Join(dir, ManifestFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["surprise"] = true
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("a manifest with an unknown field must be rejected")
	}
}

// TestVerifyCatchesATamperedTreeWithARegeneratedManifest proves the
// provenance gate is not a self-consistency check. An attacker who
// edits the tree *and* rebuilds the manifest with the tool's own digest
// algorithm still fails, because the source commit recorded in the
// manifest cannot produce the edited bytes.
//
// The source is a local git repository, so the proof needs no network
// and is deterministic. The manifest records a portable URL — a built
// template must never carry a machine-specific path — and the local
// source is passed as the fetch override, which selects where to fetch
// from without rewriting what the manifest says.
func TestVerifyCatchesATamperedTreeWithARegeneratedManifest(t *testing.T) {
	canonical, err := LoadSource(filepath.Join("..", "..", "template"))
	if err != nil {
		t.Fatal(err)
	}
	writeFiles := func(root string, files []SourceFile) {
		t.Helper()
		for _, f := range files {
			dest := filepath.Join(root, filepath.FromSlash(f.Rel))
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dest, f.Data, fileMode(f)); err != nil {
				t.Fatal(err)
			}
		}
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
			"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	// The engine source repository, standing in for the engine itself.
	src := t.TempDir()
	git(src, "init", "--quiet", "-b", "main", src)
	writeFiles(filepath.Join(src, "template"), canonical)
	git(src, "add", "-A")
	git(src, "commit", "--quiet", "-m", "source")
	commit := git(src, "rev-parse", "HEAD")

	provenance := ManifestOptions{
		RepoForgeVersion: "v1.2.3",
		SourceRef:        "v1.2.3",
		SourceCommit:     commit,
		SourceURL:        "https://example.invalid/repoforge",
	}
	out := filepath.Join(t.TempDir(), "out")
	if _, err := Build(canonical, filepath.Join(src, "template"), out, nil,
		BuildOptions{Force: true, Manifest: provenance}); err != nil {
		t.Fatalf("build: %v", err)
	}

	// A faithful build verifies: the fetch override points at the local
	// source, while the rebuilt manifest records the manifest's own URL.
	if issues, err := Verify(out, src, false); err != nil {
		t.Fatalf("a faithful build must verify: %v (%v)", err, issues)
	}

	// Tamper, then rebuild the manifest with the real digest algorithm.
	if err := os.WriteFile(filepath.Join(out, "README.md"), []byte("injected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tampered, err := LoadSource(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(out, BuildManifest(tampered, provenance)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(out); err != nil {
		t.Fatalf("a manifest rebuilt by the tool must be internally valid: %v", err)
	}

	// The tree and its manifest now agree with each other. Rebuilding
	// from the recorded source is the only thing that can catch it.
	issues, err := Verify(out, src, false)
	if err != nil {
		t.Fatalf("Verify reported an environmental failure rather than the tampering: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("a tampered tree with a regenerated manifest must not verify")
	}
}

// TestAdoptionReleasesTheProvenanceGate proves the adoption contract:
// adopting a generated repository clears its provenance so the project's
// own CI does not fail it for later adding application source or a
// go.mod. Without this the gate would freeze every adopting project.
func TestAdoptionReleasesTheProvenanceGate(t *testing.T) {
	canonical, err := LoadSource(filepath.Join("..", "..", "template"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	provenance := ManifestOptions{
		RepoForgeVersion: "v0.1.0",
		SourceRef:        "v0.1.0",
		SourceCommit:     strings.Repeat("a", 40),
		SourceURL:        "https://github.com/FernSyntax/repoforge",
	}
	if _, err := Build(canonical, filepath.Join(t.TempDir(), "src"), out, nil,
		BuildOptions{Force: true, Manifest: provenance}); err != nil {
		t.Fatalf("build: %v", err)
	}

	before, err := LoadManifest(out)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Verifiable() {
		t.Fatal("a provenance-bearing build must be verifiable before adoption")
	}

	vars := map[string]string{
		"ProjectName": "Widget", "Owner": "acme",
		"RepoURL":     "https://github.com/acme/widget",
		"Description": "A widget", "StandardVersion": "v1",
	}
	// Adopt the built tree, not the canonical source: only the built
	// tree carries a manifest to release.
	built, err := LoadSource(out)
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := Adopt(built, vars)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}

	idx := -1
	for i, f := range after {
		if f.Rel == ManifestFileName {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("the adopted tree must still carry a manifest")
	}
	var m Manifest
	if err := strictJSON(after[idx].Data, &m); err != nil {
		t.Fatalf("the adopted manifest must parse: %v", err)
	}
	if !m.Adopted {
		t.Error("the adopted manifest must be marked adopted")
	}
	if m.SourceCommit != "" || m.SourceRef != "" || m.SourceURL != "" {
		t.Errorf("an adopted manifest must record no provenance: %+v", m)
	}
	if err := m.Valid(); err != nil {
		t.Errorf("an adopted manifest must still be valid: %v", err)
	}
	if m.Verifiable() {
		t.Error("an adopted manifest must not claim to be verifiable")
	}

	// The file list must be unchanged: adoption moves the manifest to
	// adopted state, it does not stop describing the tree.
	if len(m.Files) != len(before.Files) {
		t.Errorf("adopted manifest lists %d files, the recorded list has %d", len(m.Files), len(before.Files))
	}
}

// TestAdoptedManifestIsStructurallyChecked proves an adopted manifest
// still rejects a corrupted file list.
func TestAdoptedManifestIsStructurallyChecked(t *testing.T) {
	m := BuildManifest(devFiles(), ManifestOptions{})
	adopted := AdoptManifest(m)
	adopted.Files[0].SHA256 = strings.Repeat("0", 64)
	if err := adopted.Valid(); err == nil {
		t.Error("an adopted manifest with a wrong digest must be rejected")
	}
}

// TestVerifiableFailsWhenProvenanceIsIncomplete proves the gate cannot
// be satisfied by half a provenance record.
func TestVerifiableFailsWhenProvenanceIsIncomplete(t *testing.T) {
	base := ManifestOptions{SourceCommit: strings.Repeat("a", 40)}
	if m := BuildManifest(devFiles(), base); m.Verifiable() {
		t.Error("a commit with no source URL must not be verifiable")
	}
	if m := BuildManifest(devFiles(), ManifestOptions{SourceURL: "https://x.invalid"}); m.Verifiable() {
		t.Error("a URL with no commit must not be verifiable")
	}
	m := BuildManifest(devFiles(), ManifestOptions{
		SourceCommit: strings.Repeat("a", 40), SourceURL: "https://x.invalid",
	})
	if !m.Verifiable() {
		t.Error("a commit and a URL together must be verifiable")
	}
}
