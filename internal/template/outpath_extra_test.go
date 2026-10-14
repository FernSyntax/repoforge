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

// TestUnsafeOutputErrorRendersRemedyOnlyWhenOneExists pins the operator-
// facing message: a refusal with no escape hatch must not advertise one,
// because the remedy text is the difference between "pick another
// directory" and "this target is never allowed".
func TestUnsafeOutputErrorRendersRemedyOnlyWhenOneExists(t *testing.T) {
	withRemedy := &UnsafeOutputError{
		Path:   "/srv/out",
		Reason: "is inside the template source tree",
		Remedy: "choose an output directory outside the source tree",
	}
	got := withRemedy.Error()
	for _, want := range []string{"/srv/out", "is inside the template source tree", "(choose an output directory outside the source tree)"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want it to contain %q", got, want)
		}
	}

	noRemedy := &UnsafeOutputError{Path: "/", Reason: "the filesystem root is never a build target"}
	got = noRemedy.Error()
	if strings.Contains(got, "(") || strings.Contains(got, ")") {
		t.Errorf("Error() = %q, want no remedy clause when Remedy is empty", got)
	}
	if !strings.Contains(got, "the filesystem root is never a build target") {
		t.Errorf("Error() = %q, want the reason verbatim", got)
	}
}

// TestPolicyFallbacksAreEmptyWhenTheEnvironmentIsUnresolvable proves the
// policy degrades to "no rule" rather than to a wrong rule. A missing
// home directory or working directory must not be mistaken for the
// filesystem root or for the project: either mistake would refuse a
// legitimate output directory.
func TestPolicyFallbacksAreEmptyWhenTheEnvironmentIsUnresolvable(t *testing.T) {
	t.Run("home is unresolvable", func(t *testing.T) {
		t.Setenv("HOME", "")
		if got := (OutputPolicy{}).home(); got != "" {
			t.Errorf("home() = %q, want empty when the home directory cannot be resolved", got)
		}
	})

	t.Run("working directory is unresolvable", func(t *testing.T) {
		// The only portable way to make os.Getwd fail is to unlink the
		// working directory itself, so the directory is restored before
		// the subtest ends.
		base := t.TempDir()
		gone := filepath.Join(base, "gone")
		if err := os.MkdirAll(gone, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(gone)
		if err := os.RemoveAll(gone); err != nil {
			t.Fatal(err)
		}
		if got := (OutputPolicy{}).workDir(); got != "" {
			t.Errorf("workDir() = %q, want empty when the working directory cannot be resolved", got)
		}
	})
}

// TestResolveRealPathKeepsAnUnresolvableComponentAsWritten pins the
// contract that makes the check reach a not-yet-created destination: a
// component the kernel refuses to resolve is preserved as written rather
// than aborting, so a symlink loop in an ancestor is still checked
// against the path the operator named.
func TestResolveRealPathKeepsAnUnresolvableComponentAsWritten(t *testing.T) {
	dir := t.TempDir()
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("cannot create a symlink loop: %v", err)
	}
	got, err := resolveRealPath(loop)
	if err != nil {
		t.Fatalf("resolveRealPath(%q): %v", loop, err)
	}
	if got != loop {
		t.Errorf("resolveRealPath(%q) = %q, want the path unchanged", loop, got)
	}
}

// TestWithinIsComponentAwareAndRefusesEmptySides pins the containment
// predicate the unsafe-target rules are built on. Component awareness is
// load-bearing: /a/b must never be treated as inside /a/bc, or a
// project directory would refuse a sibling that merely shares a prefix.
func TestWithinIsComponentAwareAndRefusesEmptySides(t *testing.T) {
	tests := []struct {
		name         string
		base, target string
		want         bool
	}{
		{"a path is inside itself", "/a", "/a", true},
		{"a descendant is inside", "/a", "/a/b", true},
		{"an ancestor is not inside", "/a/b", "/a", false},
		{"a sibling prefix is not inside", "/a/b", "/a/bc", false},
		{"an unrelated path is not inside", "/a", "/b", false},
		{"an empty base is never a container", "", "/a", false},
		{"an empty target is never contained", "/a", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := within(tt.base, tt.target); got != tt.want {
				t.Errorf("within(%q, %q) = %v, want %v", tt.base, tt.target, got, tt.want)
			}
		})
	}
}

// TestAncestorOfRequiresAnUnbrokenChainOfParents pins the distinction
// the source-tree rule depends on: only a path made exclusively of ".."
// steps is an ancestor. On unrelated branches filepath.Rel still returns
// a leading "..", and mistaking that for ancestry would refuse every
// directory that merely sits beside the template source tree.
func TestAncestorOfRequiresAnUnbrokenChainOfParents(t *testing.T) {
	tests := []struct {
		name      string
		anc, desc string
		want      bool
	}{
		{"a parent is an ancestor", "/a", "/a/b", true},
		{"a grandparent is an ancestor", "/a", "/a/b/c", true},
		{"a path is not its own ancestor", "/a", "/a", false},
		{"a descendant is not an ancestor", "/a/b", "/a", false},
		{"a sibling branch is not an ancestor", "/a/b", "/a/bc", false},
		{"an unrelated branch is not an ancestor", "/x", "/a/b", false},
		{"an empty ancestor is refused", "", "/a/b", false},
		{"an empty descendant is refused", "/a", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ancestorOf(tt.anc, tt.desc); got != tt.want {
				t.Errorf("ancestorOf(%q, %q) = %v, want %v", tt.anc, tt.desc, got, tt.want)
			}
		})
	}
}
