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

// TestPublishAdoptionRollsBackAWrittenFileWhenALaterWriteFails pins the
// all-or-nothing contract from the failure side: a write that cannot
// complete restores every file already written, so a failed adoption
// never leaves a repository half-adopted.
func TestPublishAdoptionRollsBackAWrittenFileWhenALaterWriteFails(t *testing.T) {
	dir := t.TempDir()
	// A regular file standing where the second write needs a
	// directory, so the second write fails after the first succeeds.
	if err := os.WriteFile(filepath.Join(dir, "blocker"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := []SourceFile{
		{Rel: "a.md", Mode: 0o644, Data: []byte("# before\n")},
	}
	after := []SourceFile{
		{Rel: "a.md", Mode: 0o644, Data: []byte("# after\n")},
		{Rel: "blocker/notes.md", Mode: 0o644, Data: []byte("# after\n")},
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PublishAdoption(dir, before, after)
	if err == nil {
		t.Fatal("an adoption whose second write cannot complete must fail")
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "a.md"))
	if readErr != nil || string(data) != "# before\n" {
		t.Fatalf("the rolled-back file must hold its pre-adoption bytes, got %q (%v)", data, readErr)
	}
}

// TestPublishAdoptionRefusesAPathEscape pins the containment check
// before any write: a relative path that resolves outside the target
// is refused by name.
func TestPublishAdoptionRefusesAPathEscape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := []SourceFile{{Rel: "a.md", Mode: 0o644, Data: []byte("# before\n")}}
	after := []SourceFile{{Rel: "../escaped.md", Mode: 0o644, Data: []byte("# after\n")}}
	_, err := PublishAdoption(dir, before, after)
	if err == nil || !strings.Contains(err.Error(), "escapes the target directory") {
		t.Fatalf("a path escape must be refused, got %v", err)
	}
}

// TestAdoptableTargetRefusesTheEngineTree pins the ancestor check: a
// target that resolves inside the engine checkout would rewrite
// template sources, and it is refused with the reason. The fixture
// adds a policy file beside the canonical ones and restores the tree
// afterwards — the canonical template/ directory is never modified.
func TestAdoptableTargetRefusesTheEngineTree(t *testing.T) {
	inside := filepath.Join("..", "..", "template")
	policyDir := filepath.Join(inside, ".repoforge")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(policyDir, "probe.yml")
	if err := os.WriteFile(probe, []byte("standard:\n  version: v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(probe) })
	if err := AdoptableTarget(inside); err == nil || !strings.Contains(err.Error(), "engine") {
		t.Fatalf("adopting inside the engine tree must be refused, got %v", err)
	}
}

// TestAdoptableTargetRefusesAnEmptyArgument pins the usage contract:
// no target directory is an error, not a silent no-op.
func TestAdoptableTargetRefusesAnEmptyArgument(t *testing.T) {
	if err := AdoptableTarget(""); err == nil {
		t.Fatal("an empty target must be refused")
	}
}
