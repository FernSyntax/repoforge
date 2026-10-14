// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"os"
	"testing"
)

const (
	schemaJSON      = "../../schemas/repoforge.schema.json"
	schemaYAML      = "../../schemas/repoforge.schema.yaml"
	govSchemaJSON   = "../../schemas/github-governance.schema.json"
	govSchemaYAML   = "../../schemas/github-governance.schema.yaml"
	jsonSchemaDraft = "https://json-schema.org/draft/2020-12/schema"
)

func loadSchema(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("schema %s is not valid JSON: %v", path, err)
	}
	return doc
}

func TestSchemaFilesExist(t *testing.T) {
	for _, p := range []string{schemaYAML, schemaJSON, govSchemaYAML, govSchemaJSON} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("schema file missing: %s: %v", p, err)
		}
		if info.Size() == 0 {
			t.Fatalf("schema file empty: %s", p)
		}
	}
}

func TestSchemaDraft(t *testing.T) {
	for _, p := range []string{schemaJSON, govSchemaJSON} {
		doc := loadSchema(t, p)
		if got := doc["$schema"]; got != jsonSchemaDraft {
			t.Fatalf("%s: $schema = %v, want %s", p, got, jsonSchemaDraft)
		}
		if doc["type"] != "object" {
			t.Fatalf("%s: root type = %v, want object", p, doc["type"])
		}
	}
}

func TestContractSchemaVersion(t *testing.T) {
	doc := loadSchema(t, schemaJSON)
	required, ok := doc["required"].([]any)
	if !ok {
		t.Fatal("root has no required list")
	}
	found := false
	for _, r := range required {
		if r == "standard" {
			found = true
		}
	}
	if !found {
		t.Fatal("root required list must contain \"standard\"")
	}

	standard, ok := doc["properties"].(map[string]any)["standard"].(map[string]any)
	if !ok {
		t.Fatal("standard section missing")
	}
	version, ok := standard["properties"].(map[string]any)["version"].(map[string]any)
	if !ok {
		t.Fatal("standard.version missing")
	}
	if version["const"] != "v1" {
		t.Fatalf("standard.version const = %v, want \"v1\"", version["const"])
	}
}

func TestGovernanceSchemaVersion(t *testing.T) {
	doc := loadSchema(t, govSchemaJSON)
	version, ok := doc["properties"].(map[string]any)["version"].(map[string]any)
	if !ok {
		t.Fatal("governance schema: version property missing")
	}
	if version["const"] != "v1" {
		t.Fatalf("governance version const = %v, want \"v1\"", version["const"])
	}
}

// TestUnknownFieldsDetectable walks every subschema and asserts that
// every object-typed node sets additionalProperties to false, so unknown
// configuration fields always fail validation (spec.md §9.2).
func TestUnknownFieldsDetectable(t *testing.T) {
	for _, p := range []string{schemaJSON, govSchemaJSON} {
		walkSchema(t, p, "$", loadSchema(t, p))
	}
}

func walkSchema(t *testing.T, file, path string, node any) {
	t.Helper()
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	if obj["type"] == "object" {
		ap, present := obj["additionalProperties"]
		if !present || ap != false {
			t.Errorf("%s %s: object schema must set additionalProperties: false", file, path)
		}
	}
	for key, val := range obj {
		switch key {
		case "properties":
			for name, sub := range val.(map[string]any) {
				walkSchema(t, file, path+".properties."+name, sub)
			}
		case "items", "additionalProperties", "unevaluatedProperties":
			walkSchema(t, file, path+"."+key, val)
		case "anyOf", "allOf", "oneOf":
			for _, sub := range val.([]any) {
				walkSchema(t, file, path+"."+key, sub)
			}
		case "$defs":
			for name, sub := range val.(map[string]any) {
				walkSchema(t, file, path+".$defs."+name, sub)
			}
		}
	}
}

func TestMergeRequiresOneMethod(t *testing.T) {
	doc := loadSchema(t, schemaJSON)
	merge, ok := doc["properties"].(map[string]any)["merge"].(map[string]any)
	if !ok {
		t.Fatal("contract merge section missing")
	}
	if anyOf, ok := merge["anyOf"].([]any); !ok || len(anyOf) == 0 {
		t.Fatal("contract merge must constrain at least one enabled merge method via anyOf")
	}

	gov := loadSchema(t, govSchemaJSON)
	govMerge, ok := gov["properties"].(map[string]any)["merge"].(map[string]any)
	if !ok {
		t.Fatal("governance merge section missing")
	}
	if anyOf, ok := govMerge["anyOf"].([]any); !ok || len(anyOf) == 0 {
		t.Fatal("governance merge must constrain at least one enabled merge method via anyOf")
	}
}
