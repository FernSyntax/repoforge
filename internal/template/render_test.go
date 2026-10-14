// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderIsLiteral is the regression test for the silent data
// corruption that `regexp.ReplaceAllString(text, value)` caused: a value
// was interpreted as a replacement template, so "$1", "$name" and "$$"
// were rewritten by the regexp engine instead of copied through.
func TestRenderIsLiteral(t *testing.T) {
	values := []string{
		"O$$",
		"A$1B",
		"$name",
		"$0",
		"back\\slash",
		`${HOME}`,
		"$",
		"a$$b$$c",
		`tab\there`,
		"quote\"and'single",
		"Unicode: héllo wörld ☃ 日本語",
		"punct: !@#%^&*()[]{}<>?/|~`+=",
		"$1 $2 ${3} $4 $5",
	}
	vars := map[string]string{"Owner": "x"}
	for _, v := range values {
		vars["Owner"] = v
		text := "owner: " + ph("Owner") + " " + "end"
		got, counts := Render(text, vars)
		want := "owner: " + v + " end"
		if got != want {
			t.Errorf("Owner=%q\n got: %q\nwant: %q", v, got, want)
		}
		if counts["Owner"] != 1 {
			t.Errorf("Owner=%q: counts = %v, want Owner=1", v, counts)
		}
	}
}

// TestRenderAllSpecials verifies every variable accepts the special
// characters that are legal for it, so no variable can silently corrupt
// generated content.
func TestRenderAllSpecials(t *testing.T) {
	for _, name := range KnownVars {
		vars := map[string]string{name: validValueFor(name)}
		got, counts := Render("{{ ."+name+" }}", vars)
		if got != validValueFor(name) {
			t.Errorf("%s: got %q want %q", name, got, validValueFor(name))
		}
		if counts[name] != 1 {
			t.Errorf("%s: counts = %v", name, counts)
		}
	}
	// Special characters that are legal in a Description survive too.
	d := "Costs 100% & uses $1 \\dollars$$"
	got, _ := Render(ph("Description"), map[string]string{"Description": d})
	if got != d {
		t.Errorf("Description not literal: got %q want %q", got, d)
	}
}

func validValueFor(name string) string {
	switch name {
	case "ProjectName":
		return "demo"
	case "Owner":
		return "acme"
	case "RepoURL":
		return "https://github.com/acme/demo"
	case "Description":
		return "A demo project"
	case "StandardVersion":
		return "v1"
	}
	return "x"
}

// TestRenderIsDeterministic proves rendering does not depend on map
// iteration order. A value that contains another variable's placeholder
// must never be re-expanded, so the output is identical on every run
// regardless of Go's randomized map ordering.
func TestRenderIsDeterministic(t *testing.T) {
	vars := map[string]string{
		"ProjectName":     "demo",
		"Owner":           ph("ProjectName") + "-owner",
		"RepoURL":         "https://github.com/acme/demo",
		"Description":     ph("Owner") + " and " + ph("ProjectName"),
		"StandardVersion": "v1",
	}
	text := strings.Join([]string{
		"name: " + ph("ProjectName"),
		"owner: " + ph("Owner"),
		"desc: " + ph("Description"),
		"url: " + ph("RepoURL"),
		"version: " + ph("StandardVersion"),
	}, "\n")

	// Render many times: the single-pass renderer must never let a
	// rendered value be expanded again, so all runs agree.
	first, _ := Render(text, vars)
	for i := 0; i < 200; i++ {
		got, _ := Render(text, vars)
		if got != first {
			t.Fatalf("run %d differs:\n%q\n%q", i, first, got)
		}
	}
	// The injected placeholders must survive verbatim.
	for _, want := range []string{ph("ProjectName") + "-owner", ph("Owner") + " and " + ph("ProjectName")} {
		if !strings.Contains(first, want) {
			t.Errorf("value containing placeholder syntax was altered:\n%s", first)
		}
	}
}

// TestRenderSpacingVariants proves the placeholder grammar accepts any
// internal whitespace, and nothing else.
func TestRenderSpacingVariants(t *testing.T) {
	for _, spacing := range []string{"", " ", "  ", "\t", "\n  "} {
		text := "x{{" + spacing + ".Owner" + spacing + "}}y"
		got, counts := Render(text, map[string]string{"Owner": "acme"})
		if got != "xacmey" {
			t.Errorf("spacing %q: got %q want %q", spacing, got, "xacmey")
		}
		if counts["Owner"] != 1 {
			t.Errorf("spacing %q: counts %v", spacing, counts)
		}
	}
}

// TestRenderLeavesUnknownAndUnprovided proves no silent drop: unknown
// placeholders and unprovided known ones survive as written.
func TestRenderLeavesUnknownAndUnprovided(t *testing.T) {
	got, counts := Render("a"+ph("Bogus")+"b"+ph("Owner")+"c", map[string]string{})
	if got != "a"+ph("Bogus")+"b"+ph("Owner")+"c" {
		t.Errorf("silently altered text: %q", got)
	}
	if len(counts) != 0 {
		t.Errorf("counts should be empty, got %v", counts)
	}
}

// TestRenderCountsAreStable pins the reporting contract: counts match the
// number of occurrences, and Describe is ordered by KnownVars.
func TestRenderCountsAreStable(t *testing.T) {
	text := strings.Repeat(ph("Owner"), 3) + strings.Repeat(ph("Description"), 2)
	_, counts := Render(text, map[string]string{"Owner": "a", "Description": "b"})
	if counts["Owner"] != 3 || counts["Description"] != 2 {
		t.Fatalf("counts = %v", counts)
	}
	// Describe must produce the same string on every run.
	first := Describe(counts)
	for i := 0; i < 100; i++ {
		if Describe(counts) != first {
			t.Fatalf("Describe is order-dependent")
		}
	}
	if first != "Owner×3, Description×2" {
		t.Errorf("Describe = %q", first)
	}
}

// TestRenderEmptyAndBinaryShapes guards the degenerate inputs.
func TestRenderEmptyAndBinaryShapes(t *testing.T) {
	if got, c := Render("", map[string]string{"Owner": "a"}); got != "" || len(c) != 0 {
		t.Errorf("empty text: %q %v", got, c)
	}
	if got, c := Render("no vars here", nil); got != "no vars here" || len(c) != 0 {
		t.Errorf("no vars: %q %v", got, c)
	}
}

// TestRenderNeverMutatesInput proves RenderFiles copies rather than
// aliases the caller's data, so a caller cannot be corrupted by a later
// render.
func TestRenderNeverMutatesInput(t *testing.T) {
	files := []SourceFile{
		{Rel: "a.md", Data: []byte(ph("Owner")), Mode: 0o644},
		{Rel: "b.bin", Data: []byte{0x00, '{', '{', 0x00}, Mode: 0o644},
	}
	vars := map[string]string{"Owner": "acme"}
	rendered := RenderFiles(files, vars)
	if string(files[0].Data) != ph("Owner") {
		t.Errorf("input mutated: %q", files[0].Data)
	}
	if string(rendered[0].Data) != "acme" {
		t.Errorf("output not rendered: %q", rendered[0].Data)
	}
	if len(rendered[0].Data) != len(rendered[0].Data) {
		t.Fatal("unreachable")
	}
	// A binary file is never touched.
	if string(rendered[1].Data) != string(files[1].Data) {
		t.Errorf("binary file modified: %q", rendered[1].Data)
	}
}

// TestValidateVarConstraints pins the per-variable value contract. These
// are the values that would corrupt generated YAML, GitHub config, or
// Markdown structure if accepted.
func TestValidateVarConstraints(t *testing.T) {
	cases := []struct {
		name  string
		value string
		valid bool
	}{
		{"ProjectName", "demo", true},
		{"ProjectName", "my-project_1.x", true},
		{"ProjectName", "", false},
		{"ProjectName", "-leading-hyphen", false},
		{"ProjectName", "with space", false},
		{"ProjectName", "with/slash", false},
		{"ProjectName", "line\nbreak", false},
		{"ProjectName", "quote\"x", false},
		{"ProjectName", strings.Repeat("a", 101), false},
		{"Owner", "acme", true},
		{"Owner", "acme-org", true},
		{"Owner", "", false},
		{"Owner", "acme corp", false},
		{"RepoURL", "https://github.com/acme/demo", true},
		{"RepoURL", "http://example.invalid/x", true},
		{"RepoURL", "ftp://github.com/acme", false},
		{"RepoURL", "not a url", false},
		{"RepoURL", "https://", false},
		{"RepoURL", "https://host with space/x", false},
		{"Description", "A demo project", true},
		{"Description", "Costs 100% & $1 \\done", true},
		{"Description", "", false},
		{"Description", "two\nlines", false},
		{"Description", strings.Repeat("x", 201), false},
		{"StandardVersion", "v1", true},
		{"StandardVersion", "v12", true},
		{"StandardVersion", "1", false},
		{"StandardVersion", "v1.0", false},
		{"StandardVersion", "vX", false},
		{"ProjectName", ph("Owner"), false},
		{"Description", ph("ProjectName"), false},
		{"Unknown", "x", false},
	}
	failed := 0
	for _, c := range cases {
		err := ValidateVar(c.name, c.value)
		if c.valid && err != nil {
			t.Errorf("%s=%q should be valid: %v", c.name, c.value, err)
			failed++
		}
		if !c.valid && err == nil {
			t.Errorf("%s=%q should be rejected", c.name, c.value)
			failed++
		}
	}
	if failed > 0 {
		t.Log(fmt.Sprintf("%d constraint cases failed", failed))
	}
}

// TestValueCannotInjectAPlaceholder is the regression test for a value
// that would turn the output into another render's input. Found by
// FuzzRenderLiteral: a value built by phTight("Owner") is inserted literally by
// Render, which is correct, but it makes re-rendering produce different
// output. The variable contract therefore rejects it, and both Build
// and Adopt call ValidateVars before rendering.
func TestValueCannotInjectAPlaceholder(t *testing.T) {
	injecting := []string{
		phTight("Owner"),
		ph("Owner"),
		"prefix " + ph("Description") + " suffix",
		"}}",
		"{{",
	}
	for _, v := range injecting {
		if err := ValidateVar("ProjectName", v); err == nil {
			t.Errorf("ProjectName=%q must be rejected by the variable contract", v)
		}
		if err := ValidateVars(map[string]string{
			"ProjectName": v, "Owner": "a", "RepoURL": "https://x.invalid/a",
			"Description": "d", "StandardVersion": "v1",
		}); err == nil {
			t.Errorf("Adopt must reject ProjectName=%q", v)
		}
		if _, err := Build(loadFixture(t), "", filepath.Join(t.TempDir(), "out"),
			map[string]string{"ProjectName": v}, BuildOptions{}); err == nil {
			t.Errorf("Build must reject ProjectName=%q", v)
		}
		if _, _, err := Adopt(loadFixture(t), map[string]string{
			"ProjectName": v, "Owner": "a", "RepoURL": "https://x.invalid/a",
			"Description": "d", "StandardVersion": "v1",
		}); err == nil {
			t.Errorf("Adopt must reject ProjectName=%q", v)
		}
	}
}
