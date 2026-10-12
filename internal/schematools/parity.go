// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

// Package schematools proves that each schema's canonical YAML form and
// its derived JSON translation parse to identical documents.
//
// The parity check used to be a Python script depending on PyYAML. That
// made the pipeline carry a third-party interpreter package, which in
// turn tripped the Pinned-Dependencies OpenSSF Scorecard check ("pip
// command not pinned by hash"), because pip installs are not
// hash-pinnable in a portable way. This package does the same comparison
// with the Go toolchain the repository already requires, so the check has
// no additional dependency, runs on any host with Go, and works under
// act — whose container image has an externally-managed Python and no
// PyYAML.
package schematools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Pair is one canonical YAML schema and its derived JSON twin.
type Pair struct {
	YAML string
	JSON string
}

// DefaultPairs is the schema pairs shipped by this repository, in a
// deterministic order so output is stable.
func DefaultPairs() []Pair {
	return []Pair{
		{"schemas/repoforge.schema.yaml", "schemas/repoforge.schema.json"},
		{"schemas/github-governance.schema.yaml", "schemas/github-governance.schema.json"},
		{"schemas/project.schema.yaml", "schemas/project.schema.json"},
	}
}

// Check verifies one pair. The two documents must parse and be deeply
// equal. A missing file is reported explicitly rather than being treated
// as drift.
func Check(p Pair) error {
	yamlData, err := os.ReadFile(p.YAML)
	if err != nil {
		return fmt.Errorf("cannot read canonical schema %s: %w", p.YAML, err)
	}
	jsonData, err := os.ReadFile(p.JSON)
	if err != nil {
		return fmt.Errorf("cannot read derived schema %s: %w", p.JSON, err)
	}
	var canonical any
	if err := yaml.Unmarshal(yamlData, &canonical); err != nil {
		return fmt.Errorf("%s is not valid YAML: %w", p.YAML, err)
	}
	var derived any
	if err := json.Unmarshal(jsonData, &derived); err != nil {
		return fmt.Errorf("%s is not valid JSON: %w", p.JSON, err)
	}
	if !equal(canonical, derived) {
		return fmt.Errorf("%s differs from %s", p.JSON, p.YAML)
	}
	return nil
}

// CheckAll verifies every pair and reports all failures, so one run
// shows the complete picture instead of only the first drift.
func CheckAll(pairs []Pair) []error {
	var errs []error
	for _, p := range pairs {
		if err := Check(p); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// equal compares two decoded documents. YAML and JSON decode different
// scalar shapes for the same value — most importantly numbers, which YAML
// decodes as int and JSON as float64 — so numeric scalars are
// normalised before comparison.
func equal(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			other, ok := bv[k]
			if !ok || !equal(v, other) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equal(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return scalarEqual(a, b)
	}
}

// scalarEqual normalises numeric scalars across the two decoders.
func scalarEqual(a, b any) bool {
	if af, aok := asFloat(a); aok {
		bf, bok := asFloat(b)
		return bok && af == bf
	}
	return a == b
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// RootFrom walks upward from dir looking for the schemas directory, so
// the command works from any working directory.
func RootFrom(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "schemas")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no schemas directory found walking up from %s", dir)
		}
		abs = parent
	}
}
