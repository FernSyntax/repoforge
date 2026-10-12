package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/orchestrate"
	"github.com/FernSyntax/repoforge/internal/project"
	"github.com/FernSyntax/repoforge/internal/template"
)

const projectDoc = `schemaVersion: v1
name: demo-site
purpose: Acceptance demonstration.
users:
  - operator
requirements:
  - id: FR-001
    text: The site builds.
exclusions:
  - SaaS operation
`

var productVars = map[string]string{
	"ProjectName": "demo-site",
	"Description": "Acceptance demonstration.",
}

// productSource locates the canonical Astro product source, skipping
// outside the checkout per repo pattern.
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

// productDeps wires the real product builder into the runner: the
// generation and verification under test.
func productDeps(t *testing.T, src string) orchestrate.Deps {
	t.Helper()
	return orchestrate.Deps{
		Validate: func(doc []byte, format string) []project.Violation {
			name := "project.yaml"
			if format == "json" {
				name = "project.json"
			}
			_, violations, err := project.LoadBytes(doc, name)
			if err != nil {
				return []project.Violation{{Field: "", Message: err.Error()}}
			}
			return violations
		},
		Generate: func(ctx context.Context, outDir string, vars map[string]string) error {
			files, err := template.LoadSource(src)
			if err != nil {
				return err
			}
			issues, err := template.BuildProduct(files, src, outDir, vars, template.BuildOptions{})
			if err != nil {
				return err
			}
			if len(issues) != 0 {
				t.Fatalf("generated tree invalid: %v", issues)
			}
			return nil
		},
		Verify: func(ctx context.Context, dir string) []template.Issue {
			m, err := template.LoadManifest(dir)
			if err != nil {
				return []template.Issue{{Rule: "manifest", Message: err.Error()}}
			}
			if m.RepoForgeVersion == "" {
				return []template.Issue{{Rule: "manifest", Message: "provenance missing engine version"}}
			}
			for _, rel := range []string{"package.json", "src/pages/index.astro", "src/site.ts"} {
				raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
				if err != nil {
					return []template.Issue{{Rule: "required", Path: rel, Message: err.Error()}}
				}
				if strings.Contains(string(raw), "{{") {
					return []template.Issue{{Rule: "placeholders", Path: rel, Message: "unresolved placeholder syntax"}}
				}
			}
			return nil
		},
	}
}

// drive runs to halt, granting the generation approval once.
func drive(t *testing.T, s *orchestrate.State, deps orchestrate.Deps) {
	t.Helper()
	for i := 0; i < 30; i++ {
		outcome, err := s.Advance(context.Background(), deps)
		if err != nil {
			t.Fatalf("advance: %v", err)
		}
		switch outcome {
		case orchestrate.OutcomeAwaitingApproval:
			if err := s.GrantApproval(orchestrate.Approval{
				Action:   orchestrate.GenerateAction,
				Artifact: s.SpecDigest,
				By:       "acceptance",
			}); err != nil {
				t.Fatalf("grant: %v", err)
			}
		case orchestrate.OutcomeHalted:
			return
		}
	}
	t.Fatal("run did not halt")
}

// TestVerticalSlice proves the composed contract: validate the
// document, approve the run, generate the product tree, verify
// required files and provenance, then rebuild byte-identically.
func TestVerticalSlice(t *testing.T) {
	src := productSource(t)
	deps := productDeps(t, src)
	out := filepath.Join(t.TempDir(), "site")

	s, err := orchestrate.NewRun("accept-1", "demo", []byte(projectDoc), "yaml", out, productVars, orchestrate.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, s, deps)
	if s.Stage != orchestrate.StageDone {
		t.Fatalf("run must finish done, at %s\n%s", s.Stage, s.Report())
	}
	if len(s.CompletedActions) != 1 {
		t.Fatalf("exactly one external action must be recorded: %v", s.CompletedActions)
	}

	// The tree is built, rendered, and provenanced.
	for _, rel := range []string{"package.json", "src/pages/index.astro", "src/site.ts", "repoforge.manifest.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("generated tree missing %s: %v", rel, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(out, "src", "site.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"demo-site"`) {
		t.Fatalf("project values must reach the output:\n%s", raw)
	}

	// Generation is repeatable: a second run converges byte-identically.
	out2 := filepath.Join(t.TempDir(), "site2")
	s2, err := orchestrate.NewRun("accept-2", "demo", []byte(projectDoc), "yaml", out2, productVars, orchestrate.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, s2, deps)
	if s2.Stage != orchestrate.StageDone {
		t.Fatalf("second run must finish done, at %s", s2.Stage)
	}
	assertIdenticalTrees(t, out, out2)
}

// TestInvalidDocumentFails proves the failure path: an invalid
// project never generates, and the report explains resumption.
func TestInvalidDocumentFails(t *testing.T) {
	src := productSource(t)
	deps := productDeps(t, src)
	bad := "schemaVersion: v1\nname: '../x'\npurpose: Demo.\n"
	s, err := orchestrate.NewRun("accept-bad", "demo", []byte(bad), "yaml", filepath.Join(t.TempDir(), "site"), productVars, orchestrate.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, s, deps)
	if s.Stage != orchestrate.StageFailed {
		t.Fatalf("invalid document must fail the run, at %s", s.Stage)
	}
	if len(s.CompletedActions) != 0 {
		t.Fatalf("failed validation must change no files: %v", s.CompletedActions)
	}
	report := s.Report()
	for _, want := range []string{"specification invalid", "files changed: none", "resumable: yes", "action required:"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report must contain %q:\n%s", want, report)
		}
	}
}

func assertIdenticalTrees(t *testing.T, a, b string) {
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
			t.Fatalf("second tree missing %s: %v", rel, err)
		}
		if string(ba) != string(bb) {
			t.Fatalf("trees differ in %s", rel)
		}
	}
}
