package template

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// tree is a snapshot of every file under a directory: relative path to
// content hash and permission bits. It is how the dangerous-path tests
// prove that a refused build changed nothing.
type tree map[string]string

func snapshotTree(t *testing.T, dir string) tree {
	t.Helper()
	out := tree{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		// A symlink is recorded as a link, never followed: writing
		// through one would make the snapshot itself destructive.
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = "symlink:" + target
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:]) + " " + d.Type().String() + " " + info.Mode().Perm().String()
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return out
}

func (tr tree) equal(other tree) bool {
	if len(tr) != len(other) {
		return false
	}
	for k, v := range tr {
		if other[k] != v {
			return false
		}
	}
	return true
}

func (tr tree) diff(other tree) string {
	var b strings.Builder
	keys := make([]string, 0, len(tr)+len(other))
	for k := range tr {
		keys = append(keys, k)
	}
	for k := range other {
		if _, ok := tr[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch {
		case tr[k] == other[k]:
			continue
		case tr[k] == "":
			b.WriteString("  + " + k + " = " + other[k] + "\n")
		case other[k] == "":
			b.WriteString("  - " + k + " (was " + tr[k] + ")\n")
		default:
			b.WriteString("  ~ " + k + ": " + tr[k] + " -> " + other[k] + "\n")
		}
	}
	return b.String()
}

// buildVars is a complete, valid variable set for the fixture.
func buildVars() map[string]string {
	return map[string]string{
		"ProjectName":     "demo",
		"Owner":           "acme",
		"RepoURL":         "https://github.com/acme/demo",
		"Description":     "A demo project",
		"StandardVersion": "v1",
	}
}

// TestOutputPolicyRejectsDangerousPaths is the filesystem-safety
// acceptance test. Every required dangerous destination is refused, and
// for every one the whole scratch tree — plus the template source tree —
// is snapshotted before and after to prove nothing outside a permitted
// output directory was removed or altered.
func TestOutputPolicyRejectsDangerousPaths(t *testing.T) {
	cases := []struct {
		name string
		// out is evaluated relative to root when it is relative.
		out  string
		home string
		// work is the working directory the policy resolves against.
		work string
	}{
		{name: "dot", out: ".", work: "<root>/work"},
		{name: "dot-slash", out: "./", work: "<root>/work"},
		{name: "dot-dot", out: "..", work: "<root>/work/nested"},
		{name: "parent-with-name", out: "../some-parent", work: "<root>/work/nested"},
		{name: "root", out: "/", work: "<root>/work"},
		{name: "root-dot", out: "/.", work: "<root>/work"},
		{name: "home", out: "<home>", home: "<home>", work: "<other>"},
		{name: "home-trailing-slash", out: "<home>/", home: "<home>", work: "<other>"},
		{name: "repository-root", out: "<root>", work: "<root>/work"},
		{name: "template-source-directory", out: "<src>", work: "<other>"},
		{name: "template-source-trailing-slash", out: "<src>/", work: "<other>"},
		{name: "ancestor-of-source-directory", out: "<srcParent>", work: "<other>"},
		{name: "ancestor-of-source-even-further", out: "<root>", work: "<other>"},
		{name: "normalized-dot-dot", out: "work/nested/../..", work: "<root>/work/nested"},
		{name: "empty", out: "", work: "<root>/work"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			// The "user data" that must survive: a nested project tree.
			victim := filepath.Join(root, "victim")
			mustMkdirAll(t, victim)
			mustWrite(t, filepath.Join(victim, "keep.txt"), "precious")
			mustMkdirAll(t, filepath.Join(victim, "sub"))
			mustWrite(t, filepath.Join(victim, "sub", "data.txt"), "more precious data")
			// The template source tree lives under the same root so an
			// ancestor/source mistake would be visible.
			src := filepath.Join(root, "engine", "template")
			mustMkdirAll(t, src)
			mustWrite(t, filepath.Join(src, "README.md"), "# source\n")
			// Working directories, one of them nested.
			mustMkdirAll(t, filepath.Join(root, "work", "nested"))
			mustMkdirAll(t, filepath.Join(root, "other"))

			home := filepath.Join(root, "home")
			out := tc.out
			out = strings.ReplaceAll(out, "<root>", root)
			out = strings.ReplaceAll(out, "<srcParent>", filepath.Dir(src))
			out = strings.ReplaceAll(out, "<src>", src)
			out = strings.ReplaceAll(out, "<home>", home)
			out = strings.ReplaceAll(out, "<other>", filepath.Join(root, "other"))
			w := tc.work
			w = strings.ReplaceAll(w, "<root>", root)
			w = strings.ReplaceAll(w, "<other>", filepath.Join(root, "other"))

			before := snapshotTree(t, root)

			policy := OutputPolicy{SourceDir: src, WorkDir: w, HomeDir: home}
			dest, err := policy.Resolve(out)
			if err == nil {
				t.Fatalf("out=%q resolved to %q; expected refusal", out, dest)
			}
			var unsafe *UnsafeOutputError
			if !asUnsafe(err, &unsafe) {
				t.Fatalf("out=%q: want *UnsafeOutputError, got %T: %v", out, err, err)
			}
			if unsafe.Reason == "" {
				t.Errorf("out=%q: refusal carries no reason", out)
			}

			// Even a forced build must not bypass an unsafe target.
			forced := OutputPolicy{SourceDir: src, WorkDir: w, HomeDir: home, Force: true}
			if _, ferr := forced.Resolve(out); ferr == nil {
				t.Errorf("out=%q: --force must not bypass an unsafe target", out)
			}

			after := snapshotTree(t, root)
			if d := before.diff(after); d != "" {
				t.Errorf("out=%q: refused build modified the filesystem:\n%s", out, d)
			}
		})
	}
}

// TestOutputPolicyAllowsDedicatedOutputInsideReverse proves the policy
// does not over-refuse: a dedicated output directory inside a project is
// accepted, including when that project is the working directory.
func TestOutputPolicyAllowsDedicatedOutputInsideProject(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "project")
	src := filepath.Join(root, "project", "template")
	mustMkdirAll(t, work)
	mustMkdirAll(t, src)
	home := filepath.Join(root, "home")
	mustMkdirAll(t, home)

	for _, out := range []string{
		"out",
		".template-out",
		filepath.Join(work, "build", "nested"),
		filepath.Join(root, "elsewhere"),
	} {
		dest, err := OutputPolicy{SourceDir: src, WorkDir: work, HomeDir: home}.Resolve(out)
		if err != nil {
			t.Errorf("out=%q: must be allowed: %v", out, err)
			continue
		}
		if !strings.HasPrefix(dest, root) {
			t.Errorf("out=%q: resolved outside the scratch root: %q", out, dest)
		}
	}
}

// TestOutputPolicyResolvesSymlinkedParents proves a path that only
// looks safe — because an existing parent is a symlink — is judged on
// its real target.
func TestOutputPolicyResolvesSymlinkedParents(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	work := filepath.Join(root, "work")
	precious := filepath.Join(home, "documents")
	mustMkdirAll(t, precious)
	mustWrite(t, filepath.Join(precious, "data.txt"), "user data")
	mustMkdirAll(t, work)
	src := filepath.Join(root, "src")
	mustMkdirAll(t, src)

	// work/into-home is a symlink into the home directory.
	link := filepath.Join(work, "into-home")
	if err := os.Symlink(precious, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	policy := OutputPolicy{SourceDir: src, WorkDir: work, HomeDir: home}
	dest, err := policy.Resolve(filepath.Join(link, "out"))
	if err != nil {
		// Refused outright is the expected answer here, and the safest.
		var unsafe *UnsafeOutputError
		if !asUnsafe(err, &unsafe) {
			t.Fatalf("want *UnsafeOutputError, got %T: %v", err, err)
		}
	} else if !strings.HasPrefix(dest, precious) {
		t.Errorf("resolved %q, expected it to be recognized as %q/...", dest, precious)
	}

	after := snapshotTree(t, root)
	if len(after) == 0 {
		t.Fatal("scratch tree was emptied")
	}
	if _, ok := after["home/documents/data.txt"]; !ok {
		t.Error("user data behind the symlink disappeared")
	}
}

// TestBuildSymlinkedOutputIsRefused pins the CLI-level symlink refusal:
// the link target must be untouched.
func TestBuildSymlinkedOutputIsRefused(t *testing.T) {
	files := loadFixture(t)
	root := t.TempDir()
	target := filepath.Join(root, "precious")
	mustMkdirAll(t, target)
	mustWrite(t, filepath.Join(target, "data.txt"), "user data")
	out := filepath.Join(root, "out")
	if err := os.Symlink(target, out); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before := snapshotTree(t, root)

	if _, err := Build(files, "", out, buildVars(), BuildOptions{Force: true}); err == nil {
		t.Fatal("building through a symlinked output path must be refused")
	}
	if d := before.diff(snapshotTree(t, root)); d != "" {
		t.Errorf("symlinked refusal modified the tree:\n%s", d)
	}
}

// TestBuildInvalidVarPreservesDestination pins the ordering guarantee:
// variables are validated before anything is deleted, so a bad value
// cannot cost the operator an existing destination.
func TestBuildInvalidVarPreservesDestination(t *testing.T) {
	files := loadFixture(t)
	out := filepath.Join(t.TempDir(), "out")
	mustMkdirAll(t, out)
	mustWrite(t, filepath.Join(out, "only-copy.txt"), "irreplaceable")
	before := snapshotTree(t, filepath.Dir(out))

	bad := []map[string]string{
		{"Bogus": "x"},
		{"ProjectName": "ok", "Owner": ""},
		{"ProjectName": "ok", "Owner": "bad owner"},
		{"ProjectName": "ok", "Owner": "acme", "Description": "line\nbreak"},
		{"ProjectName": "ok", "Owner": "acme", "RepoURL": "not-a-url"},
		{"ProjectName": "ok", "Owner": "acme", "StandardVersion": "9"},
		{"ProjectName": "ok", "Owner": "acme", "StandardVersion": "v" + ph("ProjectName")},
		{"ProjectName": "ok", "Owner": "acme", "Description": strings.Repeat("x", 500)},
	}
	for i, vars := range bad {
		if _, err := Build(files, "", out, vars, BuildOptions{Force: true}); err == nil {
			t.Errorf("case %d: invalid variables accepted", i)
		}
	}
	if d := before.diff(snapshotTree(t, filepath.Dir(out))); d != "" {
		t.Errorf("invalid variables modified the filesystem:\n%s", d)
	}
	if _, err := os.Stat(filepath.Join(out, "only-copy.txt")); err != nil {
		t.Errorf("destination content lost: %v", err)
	}
}

// TestBuildFailedValidationDoesNotPublish proves a rendered output that
// fails validation leaves the destination alone.
func TestBuildFailedValidationDoesNotPublish(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	mustMkdirAll(t, out)
	mustWrite(t, filepath.Join(out, "only-copy.txt"), "irreplaceable")

	// A source whose rendered form carries credential-shaped material.
	files := []SourceFile{
		{Rel: "README.md", Data: []byte("# ok\n"), Mode: 0o644},
		{Rel: "leak.txt", Data: []byte("token: ghp_" + strings.Repeat("a1", 18) + "\n"), Mode: 0o644},
	}
	before := snapshotTree(t, filepath.Dir(out))
	if _, err := Build(files, "", out, map[string]string{}, BuildOptions{Force: true}); err == nil {
		t.Fatal("rendered output that fails validation must fail the build")
	}
	if d := before.diff(snapshotTree(t, filepath.Dir(out))); d != "" {
		t.Errorf("failed validation modified the destination:\n%s", d)
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func asUnsafe(err error, target **UnsafeOutputError) bool {
	for err != nil {
		if u, ok := err.(*UnsafeOutputError); ok {
			*target = u
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
