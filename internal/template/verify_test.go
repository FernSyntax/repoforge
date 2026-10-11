package template

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo initializes a local git repository containing files and
// returns its path and the source commit SHA. The repository is local,
// so the provenance proof these tests exercise needs no network and is
// deterministic.
func gitRepo(t *testing.T, files []SourceFile) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
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
	run("init", "--quiet", "-b", "main", dir)
	for _, f := range files {
		dest := filepath.Join(dir, filepath.FromSlash(f.Rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, f.Data, fileMode(f)); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "source")
	return dir, run("rev-parse", "HEAD")
}

// TestValidateBuiltTreeMissingManifest proves the traceability rule: a
// tree with no manifest cannot be traced to the RepoForge source that
// produced it, so it is reported even when every other rule passes.
func TestValidateBuiltTreeMissingManifest(t *testing.T) {
	issues := ValidateBuiltTree(devFiles())
	if !hasRule(issues, "manifest/missing") {
		t.Fatalf("a built tree without a manifest must be reported, got %v", issues)
	}
}

// TestValidateBuiltTreeReservedManifestPath pins the two-phase
// validation contract: while the manifest file exists but is still
// empty (the staged tree before the manifest is written), validation
// reports the source rules only — the manifest rules cannot run
// against an empty file, and an empty manifest must not be reported
// as an invalid one.
func TestValidateBuiltTreeReservedManifestPath(t *testing.T) {
	canonical, err := LoadSource(filepath.Join("..", "..", "template"))
	if err != nil {
		t.Fatal(err)
	}
	files := append(append([]SourceFile{}, canonical...), SourceFile{
		Rel: ManifestFileName, Mode: 0o644, Data: nil,
	})
	issues := ValidateBuiltTree(files)
	for _, i := range issues {
		if strings.HasPrefix(i.Rule, "manifest/") {
			t.Fatalf("a staged tree with an empty manifest must not run the manifest rules, got %v", issues)
		}
	}
}

func TestValidateBuiltTreeRejectsAnUnparseableManifest(t *testing.T) {
	for name, data := range map[string][]byte{
		"not json":      []byte("not json at all"),
		"unknown field": []byte(`{"schema_version":"1","files":[],"unknown":1}`),
		"empty object":  []byte(`{}`),
	} {
		t.Run(name, func(t *testing.T) {
			files := append(devFiles(), SourceFile{
				Rel: ManifestFileName, Mode: 0o644, Data: data,
			})
			if !hasRule(ValidateBuiltTree(files), "manifest/invalid") {
				t.Fatalf("a manifest that fails strict decoding must be reported as invalid")
			}
		})
	}
}

// TestValidateBuiltTreeSurfacesManifestMismatch proves the manifest
// rules run on top of the source rules: a digest that does not match
// the file beside it is reported against the file, not the manifest.
func TestValidateBuiltTreeSurfacesManifestMismatch(t *testing.T) {
	files := devFiles()
	files = append(files, SourceFile{
		Rel: ManifestFileName, Mode: 0o644,
		Data: mustManifestJSON(t, BuildManifest([]SourceFile{
			{Rel: "README.md", Mode: 0o644, Data: []byte("# other\n")},
		}, ManifestOptions{RepoForgeVersion: "v0.1.0"})),
	})
	issues := ValidateBuiltTree(files)
	if !hasRule(issues, "manifest/digest") && !hasRule(issues, "manifest/extra") {
		t.Fatalf("a manifest that does not describe its tree must be reported, got %v", issues)
	}
}

func mustManifestJSON(t *testing.T, m *Manifest) []byte {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestFindSourceFile(t *testing.T) {
	files := devFiles()
	if got := findSourceFile(files, "README.md"); got == nil || got.Rel != "README.md" {
		t.Fatalf("an existing file must be found, got %v", got)
	}
	if got := findSourceFile(files, "absent.md"); got != nil {
		t.Fatalf("an absent file must return nil, got %v", got)
	}
	if got := findSourceFile(nil, "README.md"); got != nil {
		t.Fatalf("an empty tree must return nil, got %v", got)
	}
}

// TestStrictJSONRejectsUnknownFields pins the manifest decoder
// contract: a manifest that gained a field the tool does not understand
// fails loudly instead of silently dropping provenance.
func TestStrictJSONRejectsUnknownFields(t *testing.T) {
	var m Manifest
	if err := strictJSON([]byte(`{"files":[],"rogue":true}`), &m); err == nil {
		t.Fatal("an unknown field must be rejected")
	}
	if err := strictJSON([]byte(`{"files":[]}`), &m); err != nil {
		t.Fatalf("a known field must decode: %v", err)
	}
	if err := strictJSON([]byte(`{`), &m); err == nil {
		t.Fatal("malformed JSON must be rejected")
	}
}

// builtTemplate builds the canonical template with the given
// provenance and returns the built tree. The built tree is the only
// shape Verify accepts: the built-tree rules demand the full template
// file set, so a synthetic fixture never reaches the provenance paths.
func builtTemplate(t *testing.T, opts ManifestOptions) string {
	t.Helper()
	canonical, err := LoadSource(filepath.Join("..", "..", "template"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	if _, err := Build(canonical, filepath.Join("..", "..", "template"), out, nil,
		BuildOptions{Force: true, Manifest: opts}); err != nil {
		t.Fatalf("build: %v", err)
	}
	return out
}

// TestVerifyDevelopmentBuildIsValidatedButNotReproduced pins the
// development-build contract: a manifest with no source commit is
// validated fully but cannot be reproduced, so it is a note unless the
// caller demands provenance — which is what template CI does.
func TestVerifyDevelopmentBuildIsValidatedButNotReproduced(t *testing.T) {
	dir := builtTemplate(t, ManifestOptions{RepoForgeVersion: "v0.1.0", SourceRef: "dev"})
	if issues, err := Verify(dir, "", false); err != nil || len(issues) != 0 {
		t.Fatalf("a development build must verify without provenance: %v (%v)", issues, err)
	}
	_, err := Verify(dir, "", true)
	if err == nil || !strings.Contains(err.Error(), "cannot be traced") {
		t.Fatalf("requiring provenance of a development build must fail: %v", err)
	}
}

// TestVerifyCommitWithoutURLIsNotVerifiable pins the degradation: a
// manifest that records a commit but no URL cannot be fetched, so it
// is not verifiable and degrades to the development-build note rather
// than failing the run.
func TestVerifyCommitWithoutURLIsNotVerifiable(t *testing.T) {
	dir := builtTemplate(t, ManifestOptions{
		RepoForgeVersion: "v0.1.0",
		SourceRef:        "v0.1.0",
		SourceCommit:     strings.Repeat("a", 40),
	})
	if issues, err := Verify(dir, "", false); err != nil || len(issues) != 0 {
		t.Fatalf("a commit without a URL must degrade to a development build: %v (%v)", issues, err)
	}
}

// TestVerifyRefusesAnUnreachableSource proves the recorded source is
// not decorative: verification fetches that exact source, and a source
// that cannot serve it fails the run rather than passing silently.
func TestVerifyRefusesAnUnreachableSource(t *testing.T) {
	dir := builtTemplate(t, ManifestOptions{
		RepoForgeVersion: "v0.1.0",
		SourceRef:        "v0.1.0",
		SourceCommit:     strings.Repeat("a", 40),
		SourceURL:        "https://example.invalid/repoforge",
	})
	if _, err := Verify(dir, "https://example.invalid/repoforge", false); err == nil {
		t.Fatal("an unreachable recorded source must fail verification")
	}
}

// TestVerifyRoundTripAgainstALocalSource proves the full provenance
// path end to end without a network: a tree built from a local git
// repository verifies, and editing one byte while regenerating the
// manifest still fails, because the recorded commit cannot produce the
// edited content.
func TestVerifyRoundTripAgainstALocalSource(t *testing.T) {
	canonical, err := LoadSource(filepath.Join("..", "..", "template"))
	if err != nil {
		t.Fatal(err)
	}
	prefixed := make([]SourceFile, 0, len(canonical))
	for _, f := range canonical {
		f.Rel = "template/" + f.Rel
		prefixed = append(prefixed, f)
	}
	src, commit := gitRepo(t, prefixed)
	out := filepath.Join(t.TempDir(), "out")
	if _, err := Build(canonical, filepath.Join(src, "template"), out, nil, BuildOptions{
		Force: true,
		Manifest: ManifestOptions{
			RepoForgeVersion: "v1.2.3",
			SourceRef:        "v1.2.3",
			SourceCommit:     commit,
			SourceURL:        "https://example.invalid/repoforge",
		},
	}); err != nil {
		t.Fatalf("build: %v", err)
	}
	if issues, err := Verify(out, src, false); err != nil || len(issues) != 0 {
		t.Fatalf("a faithful build must verify: %v (%v)", issues, err)
	}

	if err := os.WriteFile(filepath.Join(out, "README.md"), []byte("# tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tampered, err := LoadSource(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(out, BuildManifest(tampered, ManifestOptions{
		RepoForgeVersion: "v1.2.3",
		SourceRef:        "v1.2.3",
		SourceCommit:     commit,
		SourceURL:        "https://example.invalid/repoforge",
	})); err != nil {
		t.Fatal(err)
	}
	issues, err := Verify(out, src, false)
	if err != nil {
		t.Fatalf("Verify reported an environmental failure rather than the tampering: %v", err)
	}
	if !hasRule(issues, "verify/content") {
		t.Fatalf("a tampered tree with a regenerated manifest must be caught, got %v", issues)
	}
}

// TestDiffTreesReportsEveryDifferenceShape pins the comparison
// contract: content, mode, presence on one side only — each a distinct
// finding, because a provenance gate that misses a shape is a gate that
// can be walked around.
func TestDiffTreesReportsEveryDifferenceShape(t *testing.T) {
	work := t.TempDir()
	rebuilt := filepath.Join(work, "rebuilt")
	for _, f := range []SourceFile{
		{Rel: "same.md", Mode: 0o644, Data: []byte("same\n")},
		{Rel: "content.md", Mode: 0o644, Data: []byte("fresh\n")},
		{Rel: "mode.md", Mode: 0o600, Data: []byte("m\n")},
		{Rel: "gone.md", Mode: 0o644, Data: []byte("g\n")},
	} {
		dest := filepath.Join(rebuilt, filepath.FromSlash(f.Rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, f.Data, f.Mode.Perm()); err != nil {
			t.Fatal(err)
		}
	}
	have := []SourceFile{
		{Rel: "same.md", Mode: 0o644, Data: []byte("same\n")},
		{Rel: "content.md", Mode: 0o644, Data: []byte("stale\n")},
		{Rel: "mode.md", Mode: 0o644, Data: []byte("m\n")},
		{Rel: "extra.md", Mode: 0o644, Data: []byte("e\n")},
	}
	issues, err := diffTrees(have, work)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"verify/content", "verify/mode", "verify/missing", "verify/extra"} {
		if !hasRule(issues, rule) {
			t.Errorf("diffTrees must report %s, got %v", rule, issues)
		}
	}
}

// TestDiffTreesIdenticalTreesPass pins the happy path: byte-identical
// trees, modes included, produce no findings.
func TestDiffTreesIdenticalTreesPass(t *testing.T) {
	work := t.TempDir()
	rebuilt := filepath.Join(work, "rebuilt")
	files := []SourceFile{
		{Rel: "a.md", Mode: 0o644, Data: []byte("a\n")},
		{Rel: "scripts/verify", Mode: 0o755, Data: []byte("#!/bin/sh\n")},
	}
	for _, f := range files {
		dest := filepath.Join(rebuilt, filepath.FromSlash(f.Rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, f.Data, f.Mode.Perm()); err != nil {
			t.Fatal(err)
		}
	}
	if issues, err := diffTrees(files, work); err != nil || len(issues) != 0 {
		t.Fatalf("identical trees must pass: %v (%v)", issues, err)
	}
}

func TestDiffTreesMissingRebuildIsAnError(t *testing.T) {
	if _, err := diffTrees(devFiles(), t.TempDir()); err == nil {
		t.Fatal("a missing rebuilt tree must be an error, not a finding")
	}
}

// TestLoadedSourceReadsTheTemplateSubtree pins the shape fetchSource
// leaves behind: only the template/ directory of the fetched checkout
// becomes source files.
func TestLoadedSourceReadsTheTemplateSubtree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "template"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "template", "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadedSource(root); len(got) != 1 || got[0].Rel != "README.md" {
		t.Fatalf("only template/ files must load, got %v", got)
	}
	if got := loadedSource(t.TempDir()); got != nil {
		t.Fatalf("a checkout without template/ must load nothing, got %v", got)
	}
}

// TestFetchSourceFetchesBySHA proves the fetch is pinned to the exact
// commit: the work directory ends up detached at that SHA, so a moved
// ref cannot change what gets rebuilt.
func TestFetchSourceFetchesBySHA(t *testing.T) {
	src, commit := gitRepo(t, []SourceFile{
		{Rel: "template/README.md", Mode: 0o644, Data: []byte("# demo\n")},
	})
	work := t.TempDir()
	if err := fetchSource(src, commit, work); err != nil {
		t.Fatalf("fetching a served commit must succeed: %v", err)
	}
	got := filepath.Join(work, "template", "README.md")
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("# demo\n")) {
		t.Fatalf("the fetched tree must be the recorded commit, got %q", data)
	}
	if err := fetchSource(src, strings.Repeat("b", 40), t.TempDir()); err == nil {
		t.Fatal("fetching a commit the source does not serve must fail")
	}
	if err := fetchSource("https://example.invalid/repoforge", commit, t.TempDir()); err == nil {
		t.Fatal("fetching from an unreachable source must fail")
	}
}
