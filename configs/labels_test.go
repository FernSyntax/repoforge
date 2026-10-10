package configs

import (
	"regexp"
	"testing"
)

// TestCanonicalLabels pins the deterministic label set: the exact
// names from the product definition, unique, with valid 6-digit hex
// colors and API-sized descriptions.
func TestCanonicalLabels(t *testing.T) {
	labels, err := CanonicalLabels()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"type:bug", "type:feature", "type:task", "type:docs", "type:security", "type:maintenance",
		"priority:p0", "priority:p1", "priority:p2", "priority:p3",
		"area:build", "area:ci", "area:docs", "area:github", "area:security", "area:tooling",
		"status:blocked", "status:needs-info", "status:needs-review",
	}
	if len(labels) != len(want) {
		t.Fatalf("canonical labels = %d, want %d", len(labels), len(want))
	}
	hex := regexp.MustCompile(`^[0-9a-fA-F]{6}$`)
	seen := make(map[string]bool, len(labels))
	for i, l := range labels {
		if l.Name != want[i] {
			t.Errorf("labels[%d] = %q, want %q (order is part of the contract)", i, l.Name, want[i])
		}
		if seen[l.Name] {
			t.Errorf("duplicate label %q", l.Name)
		}
		seen[l.Name] = true
		if !hex.MatchString(l.Color) {
			t.Errorf("label %q color %q is not 6-digit hex without '#'", l.Name, l.Color)
		}
		if len(l.Description) == 0 || len(l.Description) > 100 {
			t.Errorf("label %q description length %d, want 1..100", l.Name, len(l.Description))
		}
	}
}
