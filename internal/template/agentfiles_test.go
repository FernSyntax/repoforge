package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realTemplateFile reads a file from the engine's template source, so
// the framework generated repositories inherit is tested as it ships,
// not as a fixture remembers it.
func realTemplateFile(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "template", name))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("reading the template %s: %v", name, err)
	}
	return string(data)
}

// TestTemplateAgentsKeepsTheFramework proves generated repositories
// inherit the operating contract: truth hierarchy, verification,
// mandatory workflows, and the non-duplication rule.
func TestTemplateAgentsKeepsTheFramework(t *testing.T) {
	body := realTemplateFile(t, "AGENTS.md")
	for _, want := range []string{
		"# AGENTS.md",
		"## Truth hierarchy",
		"## Verification",
		"## Mandatory workflows",
		"## Rules",
		"spec.md",
		"DESIGN.md",
		"sh scripts/verify",
		"Closes #N",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("template AGENTS.md does not contain %q", want)
		}
	}
}

// TestTemplateDesignKeepsTheFrontDoor proves generated repositories
// inherit the design entry point with its change gates.
func TestTemplateDesignKeepsTheFrontDoor(t *testing.T) {
	body := realTemplateFile(t, "DESIGN.md")
	for _, want := range []string{
		"# DESIGN.md",
		"## Where design lives",
		"## How design changes",
		"## What does not go here",
		"spec.md",
		"docs/decisions/",
		"ADR-first",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("template DESIGN.md does not contain %q", want)
		}
	}
}

// TestTemplateDesignIsRequired proves the front door cannot silently
// leave the template source: the manifest demands it.
func TestTemplateDesignIsRequired(t *testing.T) {
	found := false
	for _, req := range requiredFiles {
		if req == "DESIGN.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("DESIGN.md is not in the template requiredFiles manifest")
	}
}
