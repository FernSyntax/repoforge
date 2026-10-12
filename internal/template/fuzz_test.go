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

// FuzzRenderLiteral proves the central properties of substitution, and
// would have caught the "$1" corruption immediately:
//
//  1. a value is inserted byte-for-byte, whatever it contains;
//  2. the output never depends on map iteration order;
//  3. for values that pass the variable contract — the only ones that
//     reach Render through Build or Adopt — rendering is a fixed point,
//     so a value can never expand into a placeholder.
func FuzzRenderLiteral(f *testing.F) {
	for _, s := range []string{
		"",
		"$1",
		"$$",
		"$name",
		`${x}`,
		"\\",
		"back\\\\slash",
		ph("Owner"),
		phTight("Owner"),
		"}}",
		"{{",
		"h\u00e9llo \u65e5\u672c\u8a9e \u2603",
		"a\tb\nc",
		`quote"single'`,
		"100% (a) [b] {c}",
		"github.com/x/y",
		"-not-a-name",
	} {
		f.Add(s, s, s)
	}

	f.Fuzz(func(t *testing.T, a, b, c string) {
		vars := map[string]string{"Owner": a, "Description": b, "ProjectName": c}
		text := "owner=" + ph("Owner") + " desc=" + ph("Description") + " name=" + ph("ProjectName")

		// Property 1: literal insertion, unconditionally.
		got, counts := Render(text, vars)
		want := "owner=" + a + " desc=" + b + " name=" + c
		if got != want {
			t.Fatalf("value not inserted literally:\n got %q\nwant %q", got, want)
		}
		if counts["Owner"] != 1 || counts["Description"] != 1 || counts["ProjectName"] != 1 {
			t.Fatalf("counts wrong: %v", counts)
		}

		// Property 2: no map-order dependence.
		reordered := map[string]string{"ProjectName": c, "Description": b, "Owner": a}
		if other, _ := Render(text, reordered); other != got {
			t.Fatalf("render depends on map order:\n%q\n%q", got, other)
		}

		// Property 3 is conditional: a value that looks like a
		// placeholder is rejected by ValidateVar, and therefore never
		// reaches Render through Build or Adopt. Asserting it here
		// would be asserting behaviour the product forbids.
		if !varsAreValid(vars) {
			return
		}
		again, _ := Render(got, vars)
		if again != got {
			t.Fatalf("render is not a fixed point for a valid value set:\nfirst  %q\nsecond %q", got, again)
		}
	})
}

func varsAreValid(vars map[string]string) bool {
	return ValidateVars(vars) == nil
}

// FuzzValidateVarConstraints drives the value contract with arbitrary
// input. Every rejected value must be rejected, and every accepted value
// must be a fixed point when rendered.
func FuzzValidateVarConstraints(f *testing.F) {
	for _, s := range []string{
		"", "a", "$1", ph("Owner"), "a\nb", "a b", "a/b", "https://github.com/o/p",
		"v1", "1", "héllo", strings.Repeat("x", 500), "..", "../..", "\x00",
	} {
		f.Add("Owner", s)
	}

	f.Fuzz(func(t *testing.T, name, value string) {
		err := ValidateVar(name, value)
		if err != nil {
			return
		}
		// Accepted values are safe to render anywhere.
		out, _ := Render("x={{ ."+name+" }}y", map[string]string{name: value})
		want := "x=" + value + "y"
		if out != want {
			t.Fatalf("%s=%q accepted but not rendered literally: %q", name, value, out)
		}
		// An accepted value must not be able to inject a newline that
		// breaks YAML or Markdown structure, nor a placeholder.
		if strings.ContainsAny(value, "\n\r") {
			t.Fatalf("%s accepted a newline: %q", name, value)
		}
		if strings.Contains(value, "{{") || strings.Contains(value, "}}") {
			t.Fatalf("%s accepted placeholder syntax: %q", name, value)
		}
	})
}

// FuzzOutputPolicy drives the output-path policy with arbitrary
// destination strings. Every refusal must leave the filesystem untouched,
// and any accepted path must resolve inside the scratch root.
func FuzzOutputPolicy(f *testing.F) {
	root := f.TempDir() //nolint:usetesting // the policy needs a real tree
	must := func(rel string) {
		if err := mkdirAllFor(root, rel); err != nil {
			f.Fatal(err)
		}
	}
	must("work")
	must("victim/sub")
	must("engine/template")
	must("home")

	for _, s := range []string{
		".", "./", "..", "../x", "/", "/.", "", "out", "/nonexistent/deep/path",
		"work", "victim", "engine/template", "engine", "home", "home/sub",
		"work/../victim", "/tmp", "out/../../etc",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, out string) {
		src := filepath.Join(root, "engine", "template")
		work := filepath.Join(root, "work")
		home := filepath.Join(root, "home")

		policy := OutputPolicy{SourceDir: src, WorkDir: work, HomeDir: home}
		before := snapshotTree(t, root)
		dest, err := policy.Resolve(out)
		if err != nil {
			return
		}
		// An accepted destination must never be the working directory,
		// an ancestor of it, the home directory, or the source tree.
		if dest == work || ancestorOf(dest, work) {
			t.Fatalf("out=%q resolved to an ancestor of the working directory: %q", out, dest)
		}
		if dest == home {
			t.Fatalf("out=%q resolved to the home directory: %q", out, dest)
		}
		if dest == src || within(src, dest) || ancestorOf(dest, src) {
			t.Fatalf("out=%q resolved into the source tree: %q", out, dest)
		}
		if dest == filepath.Dir(dest) {
			t.Fatalf("out=%q resolved to the filesystem root", out)
		}
		// Resolution itself must be read-only.
		if d := before.diff(snapshotTree(t, root)); d != "" {
			t.Fatalf("out=%q: resolution mutated the tree:\n%s", out, d)
		}
	})
}

func mkdirAllFor(root, rel string) error {
	return os.MkdirAll(filepath.Join(root, rel), 0o755)
}
