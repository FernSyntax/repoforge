// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package schematools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckReportsAnUnreadablePair pins the missing-file failure: a pair
// whose canonical schema is absent is reported explicitly rather than
// being treated as drift, because the remedy is "restore the file", not
// "regenerate the JSON".
func TestCheckReportsAnUnreadablePair(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "a.json")
	if err := os.WriteFile(jsonPath, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check(Pair{YAML: filepath.Join(dir, "missing.yaml"), JSON: jsonPath})
	if err == nil {
		t.Fatal("a missing canonical schema must be reported")
	}
	if want := "cannot read canonical schema"; !contains(err.Error(), want) {
		t.Errorf("err = %v, want it to mention %q", err, want)
	}
}

// TestCheckReportsAMissingDerivedSchema proves the twin case: the JSON is
// absent. It must name the derived file, because the remedy differs from
// a missing canonical schema.
func TestCheckReportsAMissingDerivedSchema(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "a.yaml")
	if err := os.WriteFile(yamlPath, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check(Pair{YAML: yamlPath, JSON: filepath.Join(dir, "missing.json")})
	if err == nil {
		t.Fatal("a missing derived schema must be reported")
	}
	if want := "cannot read derived schema"; !contains(err.Error(), want) {
		t.Errorf("err = %v, want it to mention %q", err, want)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestCheckReportsMalformedYAMLAndJSON pins the two parse failures. A
// malformed document is reported against the file that is malformed, so an
// operator editing the wrong one is told which.
func TestCheckReportsMalformedYAMLAndJSON(t *testing.T) {
	dir := t.TempDir()

	badYAML := filepath.Join(dir, "y.yaml")
	if err := os.WriteFile(badYAML, []byte("a: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goodJSON := filepath.Join(dir, "y.json")
	if err := os.WriteFile(goodJSON, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check(Pair{YAML: badYAML, JSON: goodJSON})
	if err == nil || !contains(err.Error(), "is not valid YAML") {
		t.Errorf("err = %v, want a YAML parse failure", err)
	}

	goodYAML := filepath.Join(dir, "j.yaml")
	if err := os.WriteFile(goodYAML, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badJSON := filepath.Join(dir, "j.json")
	if err := os.WriteFile(badJSON, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = Check(Pair{YAML: goodYAML, JSON: badJSON})
	if err == nil || !contains(err.Error(), "is not valid JSON") {
		t.Errorf("err = %v, want a JSON parse failure", err)
	}
}

// TestCheckAllReportsEveryFailure pins the reason CheckAll exists: one run
// shows the complete picture instead of only the first drift, so an
// operator fixing both schemas does not need two runs.
func TestCheckAllReportsEveryFailure(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	drifting := write("drift.yaml", "a: 1\n")
	drifted := write("drift.json", `{"a":2}`)
	absent := filepath.Join(dir, "absent.yaml")

	errs := CheckAll([]Pair{
		{YAML: drifting, JSON: drifted},
		{YAML: absent, JSON: drifted},
	})
	if len(errs) != 2 {
		t.Fatalf("CheckAll returned %d errors, want 2: %v", len(errs), errs)
	}
	// A clean pair contributes nothing: equivalent documents in the
	// two formats.
	cleanYAML := write("clean.yaml", "a: 1\n")
	cleanJSON := write("clean.json", `{"a":1}`)
	if errs := CheckAll([]Pair{{YAML: cleanYAML, JSON: cleanJSON}}); len(errs) != 0 {
		t.Errorf("CheckAll on a clean pair returned %v, want none", errs)
	}
}

// TestEqualRejectsShapeMismatches pins the structural comparisons a naive
// equality check would miss: a map with a different key count, a slice
// with a different length, and a scalar against a container are all
// differences even when no single value differs.
func TestEqualRejectsShapeMismatches(t *testing.T) {
	tests := []struct {
		name string
		a, b any
		want bool
	}{
		{"identical maps", map[string]any{"a": 1}, map[string]any{"a": 1}, true},
		{"a map with an extra key", map[string]any{"a": 1}, map[string]any{"a": 1, "b": 2}, false},
		{"a map missing a key", map[string]any{"a": 1, "b": 2}, map[string]any{"a": 1}, false},
		{"a differing value", map[string]any{"a": 1}, map[string]any{"a": 2}, false},
		{"identical slices", []any{1, 2}, []any{1, 2}, true},
		{"a slice with an extra element", []any{1, 2}, []any{1, 2, 3}, false},
		{"a differing element", []any{1, 2}, []any{1, 3}, false},
		{"nested maps compare recursively", map[string]any{"a": map[string]any{"b": 1}}, map[string]any{"a": map[string]any{"b": 1}}, true},
		{"nested maps differ", map[string]any{"a": map[string]any{"b": 1}}, map[string]any{"a": map[string]any{"b": 2}}, false},
		{"a map is not a slice", map[string]any{"a": 1}, []any{1}, false},
		{"a slice is not a map", []any{1}, map[string]any{"a": 1}, false},
		{"an int equals a float of the same value", 1, float64(1), true},
		{"an int64 equals a float of the same value", int64(1), float64(1), true},
		{"a uint64 equals a float of the same value", uint64(1), float64(1), true},
		{"a float32 equals a float of the same value", float32(1), float64(1), true},
		{"a number never equals a string", 1, "1", false},
		{"equal strings", "x", "x", true},
		{"differing strings", "x", "y", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equal(tt.a, tt.b); got != tt.want {
				t.Errorf("equal(%#v, %#v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestRootFromWalksUpAndFailsLoudly pins the discovery contract that lets
// the schema-parity command run from any working directory, and the
// failure when there is no schemas directory to walk up to.
func TestRootFromWalksUpAndFailsLoudly(t *testing.T) {
	t.Run("found by walking up", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "schemas"), 0o755); err != nil {
			t.Fatal(err)
		}
		deep := filepath.Join(root, "cmd", "repoforge")
		if err := os.MkdirAll(deep, 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := RootFrom(deep)
		if err != nil {
			t.Fatalf("RootFrom: %v", err)
		}
		if got != root {
			t.Errorf("RootFrom(%q) = %q, want %q", deep, got, root)
		}
	})

	t.Run("missing is reported", func(t *testing.T) {
		_, err := RootFrom(t.TempDir())
		if err == nil {
			t.Fatal("RootFrom must fail when no schemas directory exists")
		}
		if want := "no schemas directory found"; !contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	})
}
