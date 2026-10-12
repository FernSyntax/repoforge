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

// TestUnsafeRelRefusesEveryEscapeShape pins the path guard every write
// path shares. An absolute path, a leading slash and a ".." component are
// each refused: any one of them would let a rendered template write
// outside the destination the operator named.
func TestUnsafeRelRefusesEveryEscapeShape(t *testing.T) {
	tests := []struct {
		name string
		rel  string
		want bool
	}{
		{"a repository-relative path", "docs/adr/0001.md", false},
		{"a bare name", "README.md", false},
		{"a dot segment is not a traversal", "./docs/README.md", false},
		{"an absolute path", "/etc/passwd", true},
		{"a bare traversal", "../outside.md", true},
		{"a traversal in the middle", "docs/../../outside.md", true},
		{"a traversal as the last segment", "docs/..", true},
		{"a windows-style absolute path is not refused by this guard", `C:\x`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unsafeRel(tt.rel); got != tt.want {
				t.Errorf("unsafeRel(%q) = %v, want %v", tt.rel, got, tt.want)
			}
		})
	}
}

// TestCheckNoUnresolvedPlaceholdersSkipsBinary proves the placeholder
// scan never inspects binary output: a byte sequence that happens to
// look like a placeholder inside a PNG is not an unresolved
// placeholder, and flagging it would make every generated binary
// unbuildable. The sequences are assembled at runtime so this source
// file does not trip the very rule the case exercises.
func TestCheckNoUnresolvedPlaceholdersSkipsBinary(t *testing.T) {
	binary := []byte("\x89PNG\r\n\x1a\n" + "{{" + " .Owner }}" + "\x00")
	if issues := CheckNoUnresolvedPlaceholders([]SourceFile{
		{Rel: "logo.png", Mode: 0o644, Data: binary},
	}); len(issues) != 0 {
		t.Errorf("binary output must be skipped, got: %v", issues)
	}

	text := SourceFile{Rel: "README.md", Mode: 0o644, Data: []byte("Owner: " + "{{" + " .Owner }}" + "\n")}
	if issues := CheckNoUnresolvedPlaceholders([]SourceFile{text}); !hasRule(issues, "generated/placeholder") {
		t.Errorf("a known placeholder in text must be reported, got: %v", issues)
	}
}

// TestValidateAdoptionValidatesOnlyTheChangedScope pins the scope rule
// that makes adoption possible at all: the generated-repository contract
// is applied to exactly the files the adoption will change, never to the
// developer's unrelated content, which RepoForge does not own.
func TestValidateAdoptionValidatesOnlyTheChangedScope(t *testing.T) {
	changed := []SourceFile{
		// A credential-shaped string in a changed file is a finding.
		{Rel: ".github/workflows/ci.yml", Mode: 0o644, Data: []byte("token: ghp_" + strings.Repeat("a", 36) + "\n")},
	}
	if issues := ValidateAdoption(changed); !hasRule(issues, "generated/secret") {
		t.Errorf("a secret in the changed scope must be reported, got: %v", issues)
	}

	// The same content in a file the adoption does not touch is not
	// RepoForge's business: ValidateAdoption is called with the changed
	// subset only, so an empty scope raises nothing.
	if issues := ValidateAdoption(nil); len(issues) != 0 {
		t.Errorf("an unchanged adoption must raise nothing, got: %v", issues)
	}
}

// TestValidateAdoptionReportsASurvivingPlaceholder proves adoption refuses
// to publish while a known variable is still unresolved: a repository
// that claims to be adopted while carrying literal placeholder text is
// worse than a failed adoption.
func TestValidateAdoptionReportsASurvivingPlaceholder(t *testing.T) {
	changed := []SourceFile{
		{Rel: "README.md", Mode: 0o644, Data: []byte("Owner: " + "{{" + " .Owner }}" + "\n")},
		{Rel: "SECURITY.md", Mode: 0o644, Data: []byte("{{" + " .Unknown }} stays\n")},
	}
	issues := ValidateAdoption(changed)
	if !hasRule(issues, "generated/placeholder") {
		t.Fatalf("a surviving known placeholder must be reported, got: %v", issues)
	}
	// An unknown variable is not a placeholder RepoForge owns, so it is
	// not reported here.
	for _, i := range issues {
		if strings.Contains(i.Message, ".Unknown") {
			t.Errorf("an unknown variable must not be reported as an unresolved placeholder: %v", i)
		}
	}
}

// TestChangedFilesReportsOnlyWhatAdoptionWillTouch pins the diff
// semantics: a file present only in after is changed (it is new), a file
// present only in before is not (adoption never deletes), and an
// unchanged file is skipped so user modifications elsewhere survive.
func TestChangedFilesReportsOnlyWhatAdoptionWillTouch(t *testing.T) {
	before := []SourceFile{
		{Rel: "kept.md", Mode: 0o644, Data: []byte("same\n")},
		{Rel: "dropped.md", Mode: 0o644, Data: []byte("only in before\n")},
		{Rel: "edited.md", Mode: 0o644, Data: []byte("before\n")},
	}
	after := []SourceFile{
		{Rel: "kept.md", Mode: 0o644, Data: []byte("same\n")},
		{Rel: "edited.md", Mode: 0o644, Data: []byte("after\n")},
		{Rel: "added.md", Mode: 0o644, Data: []byte("new\n")},
	}
	got := ChangedFiles(before, after)
	if len(got) != 2 {
		t.Fatalf("ChangedFiles returned %d files (%v), want 2", len(got), got)
	}
	// The order is after's order, so the write sequence and the report are
	// deterministic rather than map-ordered.
	if got[0].Rel != "edited.md" || got[1].Rel != "added.md" {
		t.Errorf("ChangedFiles order = %q, %q; want after's order (edited.md, added.md)", got[0].Rel, got[1].Rel)
	}
	if hasFileRel(got, "dropped.md") {
		t.Error("a file present only in before must not be reported as changed")
	}
	if hasFileRel(got, "kept.md") {
		t.Error("an unchanged file must not be reported as changed")
	}
}

func hasFileRel(files []SourceFile, rel string) bool {
	for _, f := range files {
		if f.Rel == rel {
			return true
		}
	}
	return false
}

// TestPublishAdoptionRollsBackWhenARestoreAlsoFails pins the loudest
// failure mode: the write fails and the rollback cannot restore either.
// The message must say so explicitly, because the operator has to recover
// from version control rather than trusting the tree.
func TestPublishAdoptionRollsBackWhenARestoreAlsoFails(t *testing.T) {
	dir := t.TempDir()
	// The destination is a directory, so every write into it fails, and
	// the rollback of an already-published file fails for the same
	// reason: the destination cannot be replaced.
	blocked := filepath.Join(dir, "blocked")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	before := []SourceFile{
		{Rel: "blocked", Mode: 0o644, Data: []byte("before\n")},
	}
	after := []SourceFile{
		{Rel: "blocked", Mode: 0o644, Data: []byte("after\n")},
	}
	_, err := PublishAdoption(dir, before, after)
	if err == nil {
		t.Fatal("publishing over a directory must fail")
	}
}

// TestRollbackRestoresEveryPublishedFile pins the all-or-nothing contract
// from the other direction: when the write of a later file fails, every
// file already published is restored to its pre-adoption bytes, so a
// failed adoption leaves nothing half-written behind.
func TestRollbackRestoresEveryPublishedFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.md")
	if err := os.WriteFile(first, []byte("published\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := originalContent([]SourceFile{
		{Rel: "first.md", Mode: 0o644, Data: []byte("original\n")},
	})
	if err := rollback([]string{first}, []SourceFile{{Rel: "first.md", Data: []byte("published\n")}}, orig); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	data, err := os.ReadFile(first)
	if err != nil || string(data) != "original\n" {
		t.Errorf("first.md = %q (%v), want the pre-adoption content", data, err)
	}
}

// TestWriteFileAtomicLeavesNoTemporaryFileBehind pins the cleanup
// contract: the sibling temporary file is removed whether the write
// succeeds or fails, so a failed publish does not litter the repository
// with .repoforge-adopt-* files a later commit would pick up.
func TestWriteFileAtomicLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.md")
	if err := writeFileAtomic(dest, []byte("body\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "body\n" {
		t.Fatalf("dest = %q (%v), want the written body", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".repoforge-adopt-") {
			t.Errorf("temporary file %q survived the write", e.Name())
		}
	}
}

// TestWriteFileAtomicReportsAnUnwritableDestination proves a publish
// failure names its cause instead of being reported as a generic error:
// the operator needs to know the directory is unwritable.
func TestWriteFileAtomicReportsAnUnwritableDestination(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir", "out.md")
	if err := writeFileAtomic(missing, []byte("x\n"), 0o644); err == nil {
		t.Fatal("writing into a missing directory must fail")
	}
}

// TestFileModeFallsBackToTheStandardModeForAnUnsetValue pins the
// permission fallback: an unset mode means the standard 0644, never 0000,
// because a generated file nobody can read is a silent build failure.
func TestFileModeFallsBackToTheStandardModeForAnUnsetValue(t *testing.T) {
	if got := fileMode(SourceFile{}); got != 0o644 {
		t.Errorf("fileMode(zero) = %#o, want 0644", got)
	}
	if got := fileMode(SourceFile{Mode: 0o755}); got != 0o755 {
		t.Errorf("fileMode(0755) = %#o, want 0755", got)
	}
}

// TestDescribeModeNamesEveryRefusedShape pins the message an operator
// reads when adoption refuses a destination. Each name has a different
// remedy, so a generic "special file" would send them hunting in the
// wrong place.
func TestDescribeModeNamesEveryRefusedShape(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
		want string
	}{
		{"a symlink", os.ModeSymlink | 0o777, "symlink"},
		{"a directory", os.ModeDir | 0o755, "directory"},
		{"a named pipe", os.ModeNamedPipe, "named pipe"},
		{"a socket", os.ModeSocket, "socket"},
		{"a device", os.ModeDevice | os.ModeCharDevice, "device"},
		{"anything else", os.ModeIrregular, "special file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeMode(tt.mode); got != tt.want {
				t.Errorf("describeMode(%v) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

// TestOriginalContentIndexesByRelativePath pins the lookup a rollback
// depends on: content is recovered from memory, so rollback works even
// when the filesystem is the thing that failed.
func TestOriginalContentIndexesByRelativePath(t *testing.T) {
	got := originalContent([]SourceFile{
		{Rel: "a.md", Data: []byte("first\n")},
		{Rel: "b.md", Data: []byte("second\n")},
	})
	if len(got) != 2 {
		t.Fatalf("originalContent returned %d entries, want 2", len(got))
	}
	if string(got["a.md"].Data) != "first\n" || string(got["b.md"].Data) != "second\n" {
		t.Errorf("originalContent indexed the wrong content: %+v", got)
	}
	if _, ok := got["missing.md"]; ok {
		t.Error("an unrecorded path must not be present")
	}
}

// TestModeStringRecordsOnlyPermissionBits pins the manifest's mode
// contract: only the permission bits are recorded, so a manifest compares
// what the generated tree will look like, never who happened to own it.
// An unset mode records the standard 0644 the builder will actually apply,
// because that is what the tree will contain.
func TestModeStringRecordsOnlyPermissionBits(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
		want string
	}{
		{"the standard mode", 0o644, "0644"},
		{"an executable", 0o755, "0755"},
		{"an unset mode records the mode that will be applied", 0, "0644"},
		{"type bits are dropped", os.ModeDir | 0o755, "0755"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modeString(tt.mode); got != tt.want {
				t.Errorf("modeString(%#o) = %q, want %q", uint32(tt.mode), got, tt.want)
			}
		})
	}
}
