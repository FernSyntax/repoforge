// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// newJSONDecoder builds a decoder that rejects unknown fields, so a
// manifest carrying an unrecognized property fails loudly instead of
// silently dropping provenance.
func newJSONDecoder(r *bytes.Reader) *json.Decoder {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec
}

// ManifestFileName is the name of the generated-tree manifest inside a
// built template. It travels inside the published template repository so
// the exact RepoForge source that produced the tree is recoverable.
const ManifestFileName = "repoforge.manifest.json"

// ManifestSchemaVersion is the manifest format version. Bumping it is a
// breaking change for consumers that parse the file.
const ManifestSchemaVersion = "1"

// Manifest is the deterministic provenance record of a generated tree.
//
// It is defined so that no entry ever hashes itself: ManifestDigest is
// computed over the canonical encoding of the manifest with Digest set
// to the empty string, exactly like a detached signature over the rest
// of the document. That keeps regeneration from a recorded source
// reference byte-identical.
type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	// RepoForgeVersion is the product version that generated the tree
	// (for example "v0.1.0"). Empty for an unreleased local build.
	RepoForgeVersion string `json:"repoforge_version"`
	// SourceRef is the immutable RepoForge source reference the tree was
	// built from: a release tag or a full commit SHA.
	SourceRef string `json:"source_ref"`
	// SourceCommit is the full 40-character commit SHA, when known.
	SourceCommit string `json:"source_commit"`
	// SourceURL is the canonical engine repository URL, used by
	// verification to fetch SourceCommit.
	SourceURL string `json:"source_url"`
	// Files lists every generated file in sorted order.
	Files []ManifestFile `json:"files"`
	// Adopted marks a generated repository that has been adopted by its
	// project. Adoption releases the provenance gate: an adopted project
	// is no longer generated content, so CI must not fail it for adding
	// application source, a go.mod, or for resolving placeholders. An
	// adopted manifest carries no provenance and cannot be verified,
	// which is exactly right — there is nothing to verify it against.
	Adopted bool `json:"adopted"`

	// ManifestDigest is the SHA-256 of the canonical manifest encoding
	// with this field blank. See the type comment.
	ManifestDigest string `json:"manifest_digest"`
}

// AdoptManifest returns the manifest of an adopted repository: the
// provenance fields cleared, the adopted flag set, and the digest
// recomputed over the rest.
func AdoptManifest(m *Manifest) *Manifest {
	clone := *m
	clone.SourceRef = ""
	clone.SourceCommit = ""
	clone.SourceURL = ""
	clone.RepoForgeVersion = ""
	clone.Adopted = true
	clone.ManifestDigest = ""
	clone.ManifestDigest = clone.digest()
	return &clone
}

// Verifiable reports whether the manifest carries enough provenance for
// Verify to rebuild the tree from source. An empty provenance means a
// local development build: valid, deterministic, and explicitly not
// traceable to a release. Publication workflows must always record a
// real reference, and template CI refuses a manifest that has none.
func (m *Manifest) Verifiable() bool {
	return m.SourceCommit != "" && m.SourceURL != ""
}

// ManifestFile is one generated file: path, mode, and content digest.
type ManifestFile struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

// ManifestOptions carries the provenance a build records.
type ManifestOptions struct {
	RepoForgeVersion string
	SourceRef        string
	SourceCommit     string
	SourceURL        string
}

// BuildManifest computes the manifest for a rendered tree. Files are
// sorted here so the manifest is deterministic regardless of the order
// the caller accumulated them in.
func BuildManifest(files []SourceFile, opts ManifestOptions) *Manifest {
	sorted := append([]SourceFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Rel < sorted[j].Rel })
	m := &Manifest{
		SchemaVersion:    ManifestSchemaVersion,
		RepoForgeVersion: opts.RepoForgeVersion,
		SourceRef:        opts.SourceRef,
		SourceCommit:     opts.SourceCommit,
		SourceURL:        opts.SourceURL,
		Files:            make([]ManifestFile, 0, len(sorted)),
	}
	for _, f := range sorted {
		if f.Rel == ManifestFileName {
			continue
		}
		m.Files = append(m.Files, ManifestFile{
			Path:   f.Rel,
			Mode:   modeString(f.Mode),
			SHA256: sha256Hex(f.Data),
		})
	}
	m.ManifestDigest = m.digest()
	return m
}

// canonical encodes the manifest deterministically. The digest field is
// blanked so the document never hashes itself.
func (m *Manifest) canonical() ([]byte, error) {
	clone := *m
	clone.ManifestDigest = ""
	clone.Files = append([]ManifestFile(nil), m.Files...)
	sort.Slice(clone.Files, func(i, j int) bool { return clone.Files[i].Path < clone.Files[j].Path })
	data, err := json.Marshal(clone)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// digest recomputes the self-referential digest. A manifest whose
// recorded digest does not match has been edited after generation.
func (m *Manifest) digest() string {
	data, err := m.canonical()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Valid reports whether the manifest is internally consistent: known
// schema version, a resolvable source reference when one is recorded,
// unique sorted file paths, and a digest that matches its own contents.
//
// A manifest with no source_commit is a development build: valid and
// deterministic, but explicitly not traceable to a release. Publication
// workflows must record a real reference, and template CI refuses a
// published manifest that is not verifiable.
func (m *Manifest) Valid() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("template: manifest schema_version %q is not %q", m.SchemaVersion, ManifestSchemaVersion)
	}
	if m.Adopted {
		// An adopted manifest records no provenance by design; only its
		// structure and digest are checked.
		return m.validateStructure()
	}
	if m.SourceCommit != "" && !isFullSHA(m.SourceCommit) {
		return fmt.Errorf("template: manifest source_commit %q is not a 40-character commit SHA", m.SourceCommit)
	}
	if err := m.validateStructure(); err != nil {
		return err
	}
	if want := m.digest(); want != m.ManifestDigest {
		return fmt.Errorf("template: manifest_digest %q does not match its contents (want %q); the manifest was edited after generation", m.ManifestDigest, want)
	}
	return nil
}

// validateStructure checks the shape of the file list. It is separate
// from Valid so an adopted manifest can be structural-checked without
// also requiring provenance.
func (m *Manifest) validateStructure() error {
	seen := map[string]bool{}
	for i, f := range m.Files {
		if f.Path == "" {
			return fmt.Errorf("template: manifest file entry %d has an empty path", i)
		}
		if f.Path == ManifestFileName {
			return fmt.Errorf("template: manifest lists itself; a manifest must never hash its own record")
		}
		if strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, "..") {
			return fmt.Errorf("template: manifest path %q is not repository-relative", f.Path)
		}
		if len(f.SHA256) != 64 {
			return fmt.Errorf("template: manifest file %q has no sha256 digest", f.Path)
		}
		if seen[f.Path] {
			return fmt.Errorf("template: manifest lists %q twice", f.Path)
		}
		seen[f.Path] = true
		if i > 0 && m.Files[i-1].Path >= f.Path {
			return fmt.Errorf("template: manifest file list is not sorted at %q", f.Path)
		}
	}
	if want := m.digest(); want != m.ManifestDigest {
		return fmt.Errorf("template: manifest_digest %q does not match its contents (want %q); the manifest was edited after generation", m.ManifestDigest, want)
	}
	return nil
}

// Verify compares a built tree against the manifest that claims to
// describe it. Only paths recorded in the manifest are compared, so an
// added file is a finding and a removed file is a finding.
func (m *Manifest) Verify(files []SourceFile) []Issue {
	var issues []Issue
	if err := m.Valid(); err != nil {
		return []Issue{{Path: ManifestFileName, Rule: "manifest/invalid", Message: err.Error()}}
	}
	byPath := map[string]SourceFile{}
	for _, f := range files {
		if f.Rel == ManifestFileName {
			continue
		}
		byPath[f.Rel] = f
	}
	for _, want := range m.Files {
		got, ok := byPath[want.Path]
		if !ok {
			issues = append(issues, Issue{
				Path: want.Path, Rule: "manifest/missing",
				Message: "recorded in the manifest but absent from the generated tree",
			})
			continue
		}
		if sum := sha256Hex(got.Data); sum != want.SHA256 {
			issues = append(issues, Issue{
				Path: want.Path, Rule: "manifest/digest",
				Message: fmt.Sprintf("content digest is %s, the manifest records %s", sum, want.SHA256),
			})
			continue
		}
		if modeString(got.Mode) != want.Mode {
			issues = append(issues, Issue{
				Path: want.Path, Rule: "manifest/mode",
				Message: fmt.Sprintf("mode is %s, the manifest records %s", modeString(got.Mode), want.Mode),
			})
		}
	}
	for rel := range byPath {
		if !m.hasFile(rel) {
			issues = append(issues, Issue{
				Path: rel, Rule: "manifest/extra",
				Message: "present in the generated tree but absent from the manifest",
			})
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path })
	return issues
}

func (m *Manifest) hasFile(rel string) bool {
	for _, f := range m.Files {
		if f.Path == rel {
			return true
		}
	}
	return false
}

// LoadManifest reads and validates the manifest of a built tree.
func LoadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ManifestFileName)))
	if err != nil {
		return nil, fmt.Errorf("template: reading %s: %w", ManifestFileName, err)
	}
	var m Manifest
	dec := newJSONDecoder(bytes.NewReader(data))
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("template: %s is not a valid manifest: %w", ManifestFileName, err)
	}
	if err := m.Valid(); err != nil {
		return nil, err
	}
	return &m, nil
}

// WriteManifest serializes a manifest into dir with stable formatting.
func WriteManifest(dir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("template: encoding %s: %w", ManifestFileName, err)
	}
	data = append(data, '\n')
	path := filepath.Join(dir, filepath.FromSlash(ManifestFileName))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("template: writing %s: %w", ManifestFileName, err)
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// modeString renders the permission bits the builder will apply. Only
// the permission bits are recorded: a manifest compares what the
// generated tree will look like, never who happened to own it.
func modeString(mode os.FileMode) string {
	return fmt.Sprintf("%04o", fileMode(SourceFile{Mode: mode}).Perm())
}

func isFullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
