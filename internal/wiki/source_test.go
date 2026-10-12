// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadPagesRejectsEveryUnusableShape pins the declared contract:
// the wiki page set is a flat directory of Markdown files with usable
// names, and every other shape is refused by name — a silent skip
// would publish a partial wiki without saying which pages were lost.
func TestLoadPagesRejectsEveryUnusableShape(t *testing.T) {
	if _, err := LoadPages(""); err == nil {
		t.Fatal("an empty source directory must be refused")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPages(filepath.Join(dir, "file.md")); err == nil {
		t.Fatal("a file as the source directory must be refused")
	}
	if _, err := LoadPages(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("a missing source directory must be refused")
	}

	sub := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sub, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPages(sub); err == nil || !strings.Contains(err.Error(), "subdirectory") {
		t.Fatalf("a subdirectory must be refused, got %v", err)
	}

	nonMD := t.TempDir()
	write(t, nonMD, "notes.txt", "x\n")
	if _, err := LoadPages(nonMD); err == nil || !strings.Contains(err.Error(), PageExtension) {
		t.Fatalf("a non-Markdown file must be refused, got %v", err)
	}

	binary := t.TempDir()
	if err := os.WriteFile(filepath.Join(binary, "page.md"), []byte("a\x00b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPages(binary); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("a binary page must be refused, got %v", err)
	}
}

// TestLoadPagesReturnsSortedPages pins the determinism contract: the
// result is sorted by page name (byte order), so a plan and its commit
// do not depend on the filesystem's read order. A declared _Sidebar.md
// is a source page like any other — skipping chrome is the read-back's
// job, not the loader's.
func TestLoadPagesReturnsSortedPages(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Zebra.md", "# z\n")
	write(t, dir, "alpha.md", "# a\n")
	write(t, dir, "_Sidebar.md", "# chrome\n")
	pages, err := LoadPages(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(pages))
	for i, p := range pages {
		names[i] = p.Name
	}
	if strings.Join(names, ",") != "Zebra,_Sidebar,alpha" {
		t.Fatalf("page order = %v, want the byte-order sort [Zebra _Sidebar alpha]", names)
	}
}

// TestReadHostedSkipsTheChromeAndNonPages pins the read-back contract:
// the wiki's own sidebar and footer are never RepoForge's to rewrite,
// and non-page files are not pages.
func TestReadHostedSkipsTheChromeAndNonPages(t *testing.T) {
	clone := t.TempDir()
	write(t, clone, "Home.md", "# home\n")
	write(t, clone, "_Sidebar.md", "# sidebar\n")
	write(t, clone, "_Footer.md", "# footer\n")
	write(t, clone, "notes.txt", "not a wiki page\n")
	if err := os.MkdirAll(filepath.Join(clone, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	hosted, err := ReadHosted(clone)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosted) != 1 {
		t.Fatalf("hosted = %v, want only Home", hosted)
	}
	if string(hosted["Home"]) != "# home\n" {
		t.Fatalf("hosted[Home] = %q", hosted["Home"])
	}
	if _, err := ReadHosted(filepath.Join(clone, "absent")); err == nil {
		t.Fatal("a missing clone must be an error")
	}
}

// TestResolveSourceRefusesOutsideTheRepository pins the containment
// contract: wiki pages are published from inside the repository, and a
// source directory that resolves outside it is refused rather than
// silently cloned in.
func TestResolveSourceRefusesOutsideTheRepository(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "docs", "wiki")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSource(inside, root)
	if err != nil {
		t.Fatalf("a source inside the repository must resolve: %v", err)
	}
	if !strings.HasPrefix(got, root) {
		t.Fatalf("resolved source %q is not under %q", got, root)
	}
	outside := t.TempDir()
	if _, err := resolveSource(outside, root); err == nil ||
		!strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("a source outside the repository must be refused, got %v", err)
	}
	if _, err := resolveSource(filepath.Join(root, "absent"), root); err == nil {
		t.Fatal("a missing source directory must be refused")
	}
}
