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

// manifestFixture builds a structurally valid manifest over a known file
// set so a test can break exactly one rule and assert the message.
func manifestFixture() *Manifest {
	return BuildManifest([]SourceFile{
		{Rel: "README.md", Mode: 0o644, Data: []byte("# demo\n")},
		{Rel: "scripts/verify", Mode: 0o755, Data: []byte("#!/bin/sh\n")},
	}, ManifestOptions{})
}

// TestManifestRejectsEveryStructuralDefect pins the whole validateStructure
// contract in one table. Each case mutates a valid manifest in exactly one
// way, so a regression names the rule it broke instead of a generic
// "invalid".
func TestManifestRejectsEveryStructuralDefect(t *testing.T) {
	tests := []struct {
		name   string
		broken func(*Manifest)
		want   string
	}{
		{
			name:   "an empty path",
			broken: func(m *Manifest) { m.Files[0].Path = "" },
			want:   "has an empty path",
		},
		{
			name:   "a self entry",
			broken: func(m *Manifest) { m.Files[0].Path = ManifestFileName },
			want:   "lists itself",
		},
		{
			name:   "an absolute path",
			broken: func(m *Manifest) { m.Files[0].Path = "/etc/passwd" },
			want:   "is not repository-relative",
		},
		{
			name:   "a traversal path",
			broken: func(m *Manifest) { m.Files[0].Path = "../outside" },
			want:   "is not repository-relative",
		},
		{
			name:   "a missing digest",
			broken: func(m *Manifest) { m.Files[0].SHA256 = strings.Repeat("z", 63) },
			want:   "has no sha256 digest",
		},
		{
			name:   "a duplicate path",
			broken: func(m *Manifest) { m.Files[1].Path = m.Files[0].Path },
			want:   `lists "README.md" twice`,
		},
		{
			name:   "an unsorted list",
			broken: func(m *Manifest) { m.Files[0], m.Files[1] = m.Files[1], m.Files[0] },
			want:   "is not sorted",
		},
		{
			name:   "a stale digest",
			broken: func(m *Manifest) { m.Files[1].SHA256 = strings.Repeat("0", 64) },
			want:   "does not match its contents",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := manifestFixture()
			tt.broken(m)
			err := m.Valid()
			if err == nil {
				t.Fatal("Valid() = nil, want a structural error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Valid() = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// TestManifestAdoptedSkipsProvenanceAndStillChecksStructure pins the
// adopted shortcut: an adopted manifest carries no provenance by design,
// so only its structure and digest are checked. A malformed source commit
// is therefore not a failure for an adopted manifest, but a bad file entry
// still is.
func TestManifestAdoptedSkipsProvenanceAndStillChecksStructure(t *testing.T) {
	m := manifestFixture()
	m.SourceCommit = "deadbeef"
	adopted := AdoptManifest(m)
	if !adopted.Adopted {
		t.Fatal("AdoptManifest must set the adopted flag")
	}
	if adopted.SourceCommit != "" || adopted.SourceURL != "" || adopted.SourceRef != "" {
		t.Errorf("an adopted manifest must carry no provenance: %+v", adopted)
	}
	if err := adopted.Valid(); err != nil {
		t.Errorf("an adopted manifest with a cleared provenance must validate: %v", err)
	}

	adopted.Files[0].Path = "/absolute"
	if err := adopted.Valid(); err == nil {
		t.Error("an adopted manifest with a bad path must still be rejected")
	}
}

// TestManifestVerifyReportsMissingAndModeMismatch pins the two findings
// Verify can raise that a digest comparison alone cannot: a file the
// manifest records but the tree lacks, and a file whose permission bits
// moved without its content changing.
func TestManifestVerifyReportsMissingAndModeMismatch(t *testing.T) {
	files := manifestFiles()
	m := BuildManifest(files, ManifestOptions{})

	// A recorded file that the generated tree no longer contains.
	truncated := []SourceFile{files[1]}
	issues := m.Verify(truncated)
	if !hasRule(issues, "manifest/missing") {
		t.Errorf("issues = %v, want manifest/missing", issues)
	}

	// Same bytes, different permission bits: the digest check passes and
	// only the mode check can catch it.
	moved := []SourceFile{
		{Rel: "README.md", Mode: 0o600, Data: files[0].Data},
		files[1],
	}
	issues = m.Verify(moved)
	if !hasRule(issues, "manifest/mode") {
		t.Errorf("issues = %v, want manifest/mode", issues)
	}
	if hasRule(issues, "manifest/digest") {
		t.Errorf("an unchanged body must not be reported as a digest mismatch: %v", issues)
	}
}

func manifestFiles() []SourceFile {
	return []SourceFile{
		{Rel: "README.md", Mode: 0o644, Data: []byte("# demo\n")},
		{Rel: "scripts/verify", Mode: 0o755, Data: []byte("#!/bin/sh\n")},
	}
}

// TestManifestRoundTripRejectsAnEditedFile pins the reason the digest
// exists at all: a manifest edited after generation must not verify
// against the tree it describes, which is what stops a tampered tree from
// claiming a clean provenance record.
func TestManifestRoundTripRejectsAnEditedFile(t *testing.T) {
	dir := t.TempDir()
	m := BuildManifest(manifestFiles(), ManifestOptions{})
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir); err != nil {
		t.Fatalf("a freshly written manifest must load: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"adopted": false`, `"adopted": true`, 1)
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir); err == nil {
		t.Error("a manifest edited after generation must be refused")
	}
}

// TestLoadManifestReportsAnUnreadableTree pins the failure an operator
// actually hits: the manifest is absent. It must be reported as a read
// failure naming the file, not as a silent empty manifest.
func TestLoadManifestReportsAnUnreadableTree(t *testing.T) {
	_, err := LoadManifest(t.TempDir())
	if err == nil {
		t.Fatal("LoadManifest on a directory with no manifest must fail")
	}
	if !strings.Contains(err.Error(), ManifestFileName) {
		t.Errorf("err = %v, want it to name %s", err, ManifestFileName)
	}
}

// TestWriteManifestReportsAnUnwritableDestination proves a publication
// failure is surfaced rather than swallowed: writing into a path that is
// not a directory must fail loudly, because a silently missing manifest
// would make an otherwise valid tree unverifiable.
func TestWriteManifestReportsAnUnwritableDestination(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(notADir, manifestFixture()); err == nil {
		t.Fatal("WriteManifest into a non-directory must fail")
	}
}

// TestIsFullSHARejectsEveryNonSHA proves the provenance gate refuses a
// short, over-long, uppercase or non-hexadecimal commit reference. A
// partial SHA cannot be fetched, and an uppercase one is not what git
// records, so accepting either would break verification later.
func TestIsFullSHARejectsEveryNonSHA(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"a lowercase 40-character SHA", strings.Repeat("a", 40), true},
		{"digits and letters", "0123456789abcdef0123456789abcdef01234567", true},
		{"too short", strings.Repeat("a", 39), false},
		{"too long", strings.Repeat("a", 41), false},
		{"empty", "", false},
		{"uppercase is not a git SHA", strings.Repeat("A", 40), false},
		{"a non-hex letter", strings.Repeat("g", 40), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFullSHA(tt.in); got != tt.want {
				t.Errorf("isFullSHA(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
