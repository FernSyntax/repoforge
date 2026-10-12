package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/version"
)

var productVars = map[string]string{
	"ProjectName": "demo-site",
	"Description": "A demonstration marketing site.",
}

// productSource locates the canonical Astro product source from the
// package directory, skipping when the test runs outside the checkout.
func productSource(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "product", "astro"))
	if err != nil {
		t.Skip("not run from the engine checkout")
	}
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		t.Skip("not run from the engine checkout")
	}
	return src
}

func buildProduct(t *testing.T, src, out string, vars map[string]string, force bool) []SourceFile {
	t.Helper()
	files, err := LoadSource(src)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := BuildProduct(files, src, out, vars, BuildOptions{Force: force})
	if err != nil {
		t.Fatalf("build: %v (issues: %v)", err, issues)
	}
	if len(issues) != 0 {
		t.Fatalf("build issues: %v", issues)
	}
	return files
}

// TestLoadSourceSkipsArtifactDirs proves dependency caches and build
// outputs are never template source, however deep they nest.
func TestLoadSourceSkipsArtifactDirs(t *testing.T) {
	dir := t.TempDir()
	// The placeholder is concatenated so this file carries no literal
	// template syntax (the placeholder check scans test sources too).
	if err := os.WriteFile(filepath.Join(dir, "page.txt"), []byte("hi {{ "+".ProjectName }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, junk := range []string{"node_modules/pkg/index.js", "dist/bundle.js", ".next/out.css"} {
		p := filepath.Join(dir, filepath.FromSlash(junk))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("junk {{ "+".Nope }}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := LoadSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Rel != "page.txt" {
		t.Fatalf("only real source must load: %+v", files)
	}
}

// TestBuildProductWebStandards proves the rendered site carries the
// machine-checked SEO/AEO/GEO contract: one h1 per page, required
// meta and social tags, valid JSON-LD with no invented URLs,
// answer-engine index, and honest placeholder markers.
func TestBuildProductWebStandards(t *testing.T) {
	src := productSource(t)
	out := filepath.Join(t.TempDir(), "site")
	buildProduct(t, src, out, productVars, false)

	for _, rel := range []string{"src/pages/index.astro", "src/pages/404.astro"} {
		raw := readOut(t, out, rel)
		if n := strings.Count(raw, "<h1"); n != 1 {
			t.Fatalf("%s must carry exactly one h1, has %d", rel, n)
		}
		if !strings.Contains(raw, "<Base") {
			t.Fatalf("%s must render inside the shared layout", rel)
		}
	}

	// Layout-owned contract: landmarks, head meta, social tags, and
	// the WebSite structured-data mechanism. JSON-LD must render via
	// set:html: bare expressions inside script tags pass through
	// Astro uncompiled, which a real build proved.
	layout := readOut(t, out, "src/layouts/Base.astro")
	for _, want := range []string{
		`<html lang="en"`, `<main id="main"`, `skip-link`, `<title>`,
		`name="description"`, `name="viewport"`, `name="theme-color"`,
		`property="og:title"`, `property="og:description"`, `property="og:type"`,
		`name="twitter:card"`, `name="twitter:title"`, `name="twitter:description"`,
		`rel="icon"`, `type="application/ld+json"`, `"@type": "WebSite"`,
		`"@context": "https://schema.org"`, "set:html",
	} {
		if !strings.Contains(layout, want) {
			t.Errorf("layout missing required markup %s", want)
		}
	}
	if strings.Contains(layout, `"url"`) {
		t.Fatal("layout JSON-LD must not invent URLs")
	}

	// Page-owned contract: answer-first summary, honest FAQ, and the
	// FAQPage mechanism mirroring visible content.
	index := readOut(t, out, "src/pages/index.astro")
	for _, want := range []string{
		`id="brief-heading"`, `id="faq"`, `"@type": "FAQPage"`,
		`"@type": "Question"`, "acceptedAnswer",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index missing required content %s", want)
		}
	}
	if strings.Contains(index, `"url"`) {
		t.Fatal("FAQ JSON-LD must not invent URLs")
	}
	if !strings.Contains(index, "Replace this answer") {
		t.Fatal("FAQ must ship as labeled replace-me content, never invented facts")
	}

	for _, rel := range []string{"public/llms.txt", "public/robots.txt", "src/styles/reset.css"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("built tree missing %s: %v", rel, err)
		}
	}
	llms := readOut(t, out, "public/llms.txt")
	if !strings.Contains(llms, "demo-site") {
		t.Fatal("llms.txt must carry the project identity")
	}
}

func readOut(t *testing.T, out, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestBuildProductGoldenFileSet proves the product source ships the
// complete site contract: pages, layout, components, styles, assets,
// config, and docs.
func TestBuildProductGoldenFileSet(t *testing.T) {
	src := productSource(t)
	out := filepath.Join(t.TempDir(), "site")
	files := buildProduct(t, src, out, productVars, false)
	got := map[string]bool{}
	for _, f := range files {
		got[f.Rel] = true
	}
	for _, want := range []string{
		"package.json", "astro.config.mjs", "tsconfig.json", "README.md",
		"src/pages/index.astro", "src/pages/404.astro",
		"src/layouts/Base.astro",
		"src/components/Header.astro", "src/components/Footer.astro",
		"src/site.ts", "src/styles/global.css", "src/styles/reset.css",
		"public/favicon.svg", "public/robots.txt", "public/llms.txt",
		"package-lock.json",
	} {
		if !got[want] {
			t.Errorf("product source missing %s", want)
		}
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(want))); err != nil {
			t.Errorf("built tree missing %s: %v", want, err)
		}
	}
}

// TestBuildProductRendersVars proves project values land in the
// output and no template syntax survives.
func TestBuildProductRendersVars(t *testing.T) {
	src := productSource(t)
	out := filepath.Join(t.TempDir(), "site")
	buildProduct(t, src, out, productVars, false)
	for _, rel := range []string{"package.json", "src/site.ts", "README.md", "src/pages/index.astro"} {
		raw, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "{{") {
			t.Fatalf("%s carries unresolved placeholder syntax", rel)
		}
	}
	raw, err := os.ReadFile(filepath.Join(out, "src", "site.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"demo-site"`) || !strings.Contains(string(raw), "A demonstration marketing site.") {
		t.Fatalf("vars not rendered:\n%s", raw)
	}
}

// TestBuildProductRequiresVars proves a missing value fails closed
// before anything is written.
func TestBuildProductRequiresVars(t *testing.T) {
	src := productSource(t)
	files, err := LoadSource(src)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "site")
	if _, err := BuildProduct(files, src, out, nil, BuildOptions{}); err == nil {
		t.Fatal("missing vars must fail the build")
	} else if !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("failure must name placeholders: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatal("failed build must not create the destination")
	}
}

// TestBuildProductRejectsUnknownVar proves the known-var gate applies
// to product builds too.
func TestBuildProductRejectsUnknownVar(t *testing.T) {
	src := productSource(t)
	files, err := LoadSource(src)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"ProjectName": "x", "Description": "y", "Sneaky": "z"}
	if _, err := BuildProduct(files, src, filepath.Join(t.TempDir(), "site"), vars, BuildOptions{}); err == nil {
		t.Fatal("unknown var must fail the build")
	}
}

// TestBuildProductManifest proves provenance is recorded with the
// engine version.
func TestBuildProductManifest(t *testing.T) {
	src := productSource(t)
	out := filepath.Join(t.TempDir(), "site")
	buildProduct(t, src, out, productVars, false)
	m, err := LoadManifest(out)
	if err != nil {
		t.Fatalf("manifest must load: %v", err)
	}
	if m.RepoForgeVersion != version.Version {
		t.Fatalf("manifest must record engine version %q, got %q", version.Version, m.RepoForgeVersion)
	}
	if len(m.Files) == 0 {
		t.Fatal("manifest must list generated files")
	}
}

// TestBuildProductDeterministic proves two builds are byte-identical,
// including the manifest.
func TestBuildProductDeterministic(t *testing.T) {
	src := productSource(t)
	dir := t.TempDir()
	out1 := filepath.Join(dir, "a")
	out2 := filepath.Join(dir, "b")
	buildProduct(t, src, out1, productVars, false)
	buildProduct(t, src, out2, productVars, false)
	assertTreesIdentical(t, out1, out2)
}

func assertTreesIdentical(t *testing.T, a, b string) {
	t.Helper()
	var rels []string
	if err := filepath.Walk(a, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(a, path)
		if err != nil {
			return err
		}
		rels = append(rels, rel)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range rels {
		ba, err := os.ReadFile(filepath.Join(a, rel))
		if err != nil {
			t.Fatal(err)
		}
		bb, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil {
			t.Fatalf("second build missing %s: %v", rel, err)
		}
		if string(ba) != string(bb) {
			t.Fatalf("builds differ in %s", rel)
		}
	}
	var count int
	if err := filepath.Walk(b, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != len(rels) {
		t.Fatalf("builds have different file counts: %d vs %d", len(rels), count)
	}
}

// TestBuildProductRefusals proves destination safety: the source
// itself, parents, absolute escapes, and non-empty targets without
// force never write.
func TestBuildProductRefusals(t *testing.T) {
	src := productSource(t)
	files, err := LoadSource(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{".", "..", "/", src} {
		if _, err := BuildProduct(files, src, target, productVars, BuildOptions{}); err == nil {
			t.Fatalf("target %q must be refused", target)
		}
	}
	out := filepath.Join(t.TempDir(), "site")
	buildProduct(t, src, out, productVars, false)
	if _, err := BuildProduct(files, src, out, productVars, BuildOptions{}); err == nil {
		t.Fatal("non-empty destination without force must be refused")
	}
	if _, err := BuildProduct(files, src, out, productVars, BuildOptions{Force: true}); err != nil {
		t.Fatalf("force rebuild must succeed: %v", err)
	}
}
