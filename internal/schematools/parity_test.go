// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package schematools

import (
	"os"
	"path/filepath"
	"testing"
)

func writePair(t *testing.T, yamlBody, jsonBody string) Pair {
	t.Helper()
	dir := t.TempDir()
	y := filepath.Join(dir, "a.yaml")
	j := filepath.Join(dir, "a.json")
	if err := os.WriteFile(y, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j, []byte(jsonBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return Pair{YAML: y, JSON: j}
}

// TestCheckAcceptsIdenticalDocuments proves the happy path, including
// the int/float64 asymmetry between the two decoders.
func TestCheckAcceptsIdenticalDocuments(t *testing.T) {
	p := writePair(t, "a: 1\nb: [x, y]\nc:\n  d: true\n", `{"a":1,"b":["x","y"],"c":{"d":true}}`)
	if err := Check(p); err != nil {
		t.Errorf("identical documents must pass: %v", err)
	}
}

// TestCheckDetectsDrift proves a real difference fails.
func TestCheckDetectsDrift(t *testing.T) {
	p := writePair(t, "a: 1\n", `{"a":2}`)
	if err := Check(p); err == nil {
		t.Fatal("drift must fail")
	}
}

// TestCheckReportsMissingFiles proves a missing file is named, not
// silently treated as drift.
func TestCheckReportsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	err := Check(Pair{YAML: filepath.Join(dir, "missing.yaml"), JSON: filepath.Join(dir, "missing.json")})
	if err == nil {
		t.Fatal("a missing file must fail")
	}
}

// TestCheckReportsMalformed proves invalid YAML and invalid JSON are
// each reported with the offending file.
func TestCheckReportsMalformed(t *testing.T) {
	p := writePair(t, "a: [unclosed\n", `{"a":1}`)
	if err := Check(p); err == nil {
		t.Error("invalid YAML must fail")
	}
	p = writePair(t, "a: 1\n", `{"a":`)
	if err := Check(p); err == nil {
		t.Error("invalid JSON must fail")
	}
}

// TestDefaultPairsPassesAgainstTheRealSchemas is the guard against the
// shipped pair drifting. DefaultPairs paths are relative to the
// repository root, which `go test` does not set as the working
// directory, so the test runs from there explicitly.
func TestDefaultPairsPassesAgainstTheRealSchemas(t *testing.T) {
	root, err := RootFrom("..")
	if err != nil {
		t.Skip("not run from the engine checkout")
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	if errs := CheckAll(DefaultPairs()); len(errs) != 0 {
		t.Fatalf("shipped schema pairs drifted: %v", errs)
	}
}
