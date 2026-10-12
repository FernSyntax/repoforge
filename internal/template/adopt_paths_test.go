// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateGeneratedFlagsEveryRule covers the generated-repository
// contract rule by rule. Each row is a defect that must never be
// written into a repository RepoForge touches, whatever the adopting
// project legitimately owns.
func TestValidateGeneratedFlagsEveryRule(t *testing.T) {
	for _, tc := range []struct {
		name string
		rel  string
		data string
		rule string
	}{
		{
			name: "path escapes the target",
			rel:  "../escape",
			data: "x\n",
			rule: "generated/path",
		},
		{
			name: "credential-shaped material",
			rel:  "notes.md",
			data: "token: " + "ghp_" + strings.Repeat("a1", 18) + "\n",
			rule: "generated/secret",
		},
		{
			name: "hardcoded identity",
			rel:  "CODEOWNERS",
			data: "* " + "maintainer" + "@" + "example.com\n",
			rule: "generated/identity",
		},
		{
			name: "machine-specific absolute path",
			rel:  "notes.md",
			data: "lives at /home/operator/src\n",
			rule: "generated/abs-path",
		},
		{
			name: "unparseable YAML",
			rel:  ".repoforge/config.yml",
			data: "standard:\n  version: [unclosed\n",
			rule: "generated/yaml",
		},
		{
			name: "unpinned action",
			rel:  ".github/workflows/ci.yml",
			data: "jobs:\n  v:\n    steps:\n      - uses: actions/checkout@v4\n",
			rule: "generated/pinning",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if issues := ValidateGenerated([]SourceFile{{Rel: tc.rel, Mode: 0o644, Data: []byte(tc.data)}}); !hasRule(issues, tc.rule) {
				t.Fatalf("expected %s issue, got %+v", tc.rule, issues)
			}
		})
	}
}

// TestValidateGeneratedSkipsBinaryFiles pins that a file with no text
// content is never content-checked: a digest match is the only rule
// that applies to it, and a secret-shaped byte sequence inside binary
// data is not a credential leak.
func TestValidateGeneratedSkipsBinaryFiles(t *testing.T) {
	// The classic PNG magic number, which carries a NUL byte.
	files := []SourceFile{{Rel: "logo.png", Mode: 0o644, Data: []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}}}
	if issues := ValidateGenerated(files); len(issues) != 0 {
		t.Errorf("a binary file must be skipped, got %+v", issues)
	}
}

// TestPublishAdoptionRefusesANonRegularDestination pins the destination
// gate that makes adoption all-or-nothing: every destination is checked
// before anything is written, so a baseline path that is not a regular
// file is refused instead of being written through.
func TestPublishAdoptionRefusesANonRegularDestination(t *testing.T) {
	dir := t.TempDir()
	// The refused destination is a directory standing where a baseline
	// file belongs.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := []SourceFile{
		{Rel: "README.md", Mode: 0o644, Data: []byte("# before\n")},
	}
	after := []SourceFile{
		{Rel: "README.md", Mode: 0o644, Data: []byte("# after\n")},
		{Rel: "sub", Mode: 0o644, Data: []byte("# after\n")},
	}
	// Only "sub" is a changed destination: README.md exists on disk, so
	// the gate must refuse on the directory alone and write nothing.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PublishAdoption(dir, before, after)
	if err == nil {
		t.Fatal("a directory at a destination must be refused before any write")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("the error must name the refused destination shape, got %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "README.md")); err != nil || string(data) != "# before\n" {
		t.Errorf("a refused adoption must not write anything, got %q (%v)", data, err)
	}
}

// TestPublishAdoptionRollsBackWhenAWriteFails pins the rollback half of
// the write loop: a file published before a failure is restored from
// the in-memory pre-adoption content, so a failed adoption is not a
// half-adopted repository.
//
// The failing destination is a directory standing where a baseline file
// belongs, so the atomic write cannot create its temporary file beside
// the destination and the write loop fails mid-publish.
func TestPublishAdoptionRollsBackWhenAWriteFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "notes.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := []SourceFile{
		{Rel: "README.md", Mode: 0o644, Data: []byte("# before\n")},
		{Rel: "docs/notes.md", Mode: 0o644, Data: []byte("notes\n")},
	}
	after := []SourceFile{
		{Rel: "README.md", Mode: 0o644, Data: []byte("# after\n")},
		{Rel: "docs/notes.md", Mode: 0o644, Data: []byte("notes adopted\n")},
	}
	// README.md is published first, then the write for docs/notes.md
	// fails, and the rollback must restore README.md.
	if err := os.RemoveAll(filepath.Join(dir, "docs", "notes.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs", "notes.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := PublishAdoption(dir, before, after)
	if err == nil {
		t.Fatal("a write that cannot be published must fail")
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "README.md"))
	if readErr != nil || string(data) != "# before\n" {
		t.Errorf("the rolled-back adoption did not restore README.md: %q (%v)", data, readErr)
	}
}

// TestFileModeDefaultsToTheStandardMode pins the zero-value contract:
// a source file recorded without a mode is published world-readable,
// which is what every text file in a generated repository should be.
func TestFileModeDefaultsToTheStandardMode(t *testing.T) {
	if got := fileMode(SourceFile{Mode: 0}); got != 0o644 {
		t.Errorf("a file with no mode must default to 0644, got %#o", got)
	}
	if got := fileMode(SourceFile{Mode: 0o755}); got != 0o755 {
		t.Errorf("an executable mode must be preserved, got %#o", got)
	}
}

// TestDescribeModeNamesTheRefusedShape pins that the refusal message
// names what it refused, so an operator who finds a directory where a
// baseline file belongs is told that rather than a bare stat code.
func TestDescribeModeNamesTheRefusedShape(t *testing.T) {
	for _, tc := range []struct {
		mode os.FileMode
		want string
	}{
		{os.ModeSymlink, "symlink"},
		{os.ModeDir, "directory"},
		{os.ModeNamedPipe, "named pipe"},
		{os.ModeSocket, "socket"},
		{os.ModeDevice, "device"},
		{os.ModeIrregular, "special file"},
	} {
		if got := describeMode(tc.mode); got != tc.want {
			t.Errorf("describeMode(%v) = %q, want %q", tc.mode, got, tc.want)
		}
	}
}

// TestRollbackRestoresFromMemory pins that the rollback recovers the
// pre-adoption content from the in-memory copy rather than from disk,
// which is what makes it work when the filesystem is the thing that
// failed.
func TestRollbackRestoresFromMemory(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "README.md")
	if err := os.WriteFile(dest, []byte("# after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := []SourceFile{{Rel: "README.md", Mode: 0o644, Data: []byte("# after\n")}}
	orig := originalContent([]SourceFile{
		{Rel: "README.md", Mode: 0o644, Data: []byte("# before\n")},
		// A path absent from the rollback list must be skipped, not
		// written: the diff decides what to restore.
		{Rel: "untouched.md", Mode: 0o644, Data: []byte("x\n")},
	})
	if err := rollback([]string{dest}, changed, orig); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "# before\n" {
		t.Errorf("the pre-adoption content was not restored: %q (%v)", data, err)
	}
	// An empty rollback list restores nothing and reports no error.
	if err := rollback(nil, changed, orig); err != nil {
		t.Errorf("an empty rollback must succeed: %v", err)
	}
	// A rollback with nothing to restore for a path is a no-op, not a
	// failure.
	if err := rollback([]string{dest}, changed, nil); err != nil {
		t.Errorf("a rollback with no recorded original must succeed: %v", err)
	}
}

// TestWriteFileAtomicReportsAnUnstageableDirectory pins that a
// destination whose parent cannot hold a temporary file is reported,
// not written through: the sibling temporary file is the mechanism that
// keeps a reader from ever seeing a half-written file.
func TestWriteFileAtomicReportsAnUnstageableDirectory(t *testing.T) {
	dir := t.TempDir()
	afile := filepath.Join(dir, "afile")
	if err := os.WriteFile(afile, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(afile, "out.md"), []byte("data\n"), 0o644); err == nil {
		t.Fatal("a destination whose parent is a file must be refused")
	}
}
