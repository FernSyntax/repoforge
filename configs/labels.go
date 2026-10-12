// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package configs

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// CanonicalLabel is one entry of the deterministic repository label
// set (configs/github/labels.yaml). Color is hexadecimal without a
// leading '#', matching the labels API convention.
type CanonicalLabel struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Description string `yaml:"description"`
}

// CanonicalLabels loads the canonical label set. Unknown fields are
// rejected and duplicate names are an error: the document is the
// single source of truth bootstrap applies and doctor audits.
func CanonicalLabels() ([]CanonicalLabel, error) {
	data, err := GitHub.ReadFile("github/labels.yaml")
	if err != nil {
		return nil, fmt.Errorf("configs: canonical labels: %w", err)
	}
	var doc struct {
		Version string           `yaml:"version"`
		Labels  []CanonicalLabel `yaml:"labels"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("configs: canonical labels: %w", err)
	}
	if doc.Version != "v1" {
		return nil, fmt.Errorf("configs: canonical labels: unknown version %q", doc.Version)
	}
	seen := make(map[string]bool, len(doc.Labels))
	for _, l := range doc.Labels {
		if l.Name == "" {
			return nil, fmt.Errorf("configs: canonical labels: label with empty name")
		}
		if seen[l.Name] {
			return nil, fmt.Errorf("configs: canonical labels: duplicate label %q", l.Name)
		}
		seen[l.Name] = true
	}
	return doc.Labels, nil
}
