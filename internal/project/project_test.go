package project

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const validYAML = `schemaVersion: v1
name: demo-site
purpose: Demonstrate the project model.
users:
  - maintainer
  - contributor
requirements:
  - id: FR-001
    text: The site builds.
  - id: FR-002
    text: The site is accessible.
exclusions:
  - SaaS operation
`

const validJSON = `{"schemaVersion":"v1","name":"demo-site","purpose":"Demonstrate the project model.","users":["maintainer","contributor"],"requirements":[{"id":"FR-001","text":"The site builds."},{"id":"FR-002","text":"The site is accessible."}],"exclusions":["SaaS operation"]}`

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadValidYAML proves the happy path and the decoded values.
func TestLoadValidYAML(t *testing.T) {
	p, violations, err := LoadFile(writeTemp(t, "project.yaml", validYAML))
	if err != nil {
		t.Fatalf("valid document must load: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("valid document must have no violations: %v", violations)
	}
	if p.SchemaVersion != "v1" || p.Name != "demo-site" || len(p.Requirements) != 2 {
		t.Fatalf("decoded values wrong: %+v", p)
	}
}

// TestYAMLJSONEquivalence proves both encodings decode to the same
// project, so the schema pair is one model, not two.
func TestYAMLJSONEquivalence(t *testing.T) {
	dir := t.TempDir()
	yPath := filepath.Join(dir, "project.yaml")
	jPath := filepath.Join(dir, "project.json")
	if err := os.WriteFile(yPath, []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jPath, []byte(validJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	py, vy, err := LoadFile(yPath)
	if err != nil || len(vy) != 0 {
		t.Fatalf("yaml must load clean: %v %v", err, vy)
	}
	pj, vj, err := LoadFile(jPath)
	if err != nil || len(vj) != 0 {
		t.Fatalf("json must load clean: %v %v", err, vj)
	}
	if !reflect.DeepEqual(py, pj) {
		t.Fatalf("yaml and json must decode identically:\n%+v\n%+v", py, pj)
	}
}

// TestUnknownFieldRejected proves strict decoding in both encodings:
// an unrecognized field is a violation, never silently accepted.
func TestUnknownFieldRejected(t *testing.T) {
	yamlDoc := validYAML + "sneaky: true\n"
	jsonDoc := strings.TrimSuffix(validJSON, "}") + `,"sneaky":true}`
	for _, tc := range []struct{ name, file, body string }{
		{"yaml", "project.yaml", yamlDoc},
		{"json", "project.json", jsonDoc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, violations, err := LoadFile(writeTemp(t, tc.file, tc.body))
			if err != nil {
				t.Fatalf("unknown field must be a violation, not a syntax error: %v", err)
			}
			if len(violations) == 0 {
				t.Fatal("unknown field must produce a violation")
			}
		})
	}
}

// TestUnknownVersionRejected proves an unrecognized schema version
// fails closed instead of loading under v1 semantics.
func TestUnknownVersionRejected(t *testing.T) {
	doc := strings.Replace(validYAML, "schemaVersion: v1", "schemaVersion: v9", 1)
	_, violations, err := LoadFile(writeTemp(t, "project.yaml", doc))
	if err != nil {
		t.Fatalf("unknown version must be a violation, not a syntax error: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("unknown version must produce a violation")
	}
}

// TestMissingRequired proves each required field is enforced.
func TestMissingRequired(t *testing.T) {
	base := map[string]string{
		"schemaVersion": "schemaVersion: v1\n",
		"name":          "name: demo-site\n",
		"purpose":       "purpose: Demonstrate the project model.\n",
	}
	for field, line := range base {
		t.Run("missing-"+field, func(t *testing.T) {
			doc := strings.Replace(validYAML, line, "", 1)
			_, violations, err := LoadFile(writeTemp(t, "project.yaml", doc))
			if err != nil {
				t.Fatalf("missing field must be a violation: %v", err)
			}
			if len(violations) == 0 {
				t.Fatalf("missing %s must produce a violation", field)
			}
		})
	}
}

// TestUnsafeNameRejected proves the name cannot escape its directory:
// traversal sequences, absolute paths, dots, and case all fail.
func TestUnsafeNameRejected(t *testing.T) {
	for _, name := range []string{"../escape", "..", "/abs", "UPPER", "has space", "dot.name", "under_score", ""} {
		t.Run("name-"+name, func(t *testing.T) {
			doc := strings.Replace(validYAML, "name: demo-site", "name: "+name, 1)
			// An empty name line still parses (null); every case here
			// must fail validation, none may fail parsing.
			_, violations, err := LoadFile(writeTemp(t, "project.yaml", doc))
			if err != nil {
				t.Fatalf("name %q must be a violation, not a syntax error: %v", name, err)
			}
			if len(violations) == 0 {
				t.Fatalf("name %q must produce a violation", name)
			}
		})
	}
}

// TestDuplicateRequirementIDs proves identifier uniqueness is enforced.
func TestDuplicateRequirementIDs(t *testing.T) {
	doc := strings.Replace(validYAML, "- id: FR-002", "- id: FR-001", 1)
	_, violations, err := LoadFile(writeTemp(t, "project.yaml", doc))
	if err != nil {
		t.Fatalf("duplicate IDs must be a violation: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("duplicate requirement IDs must produce a violation")
	}
}

// TestBadRequirementIDFormat proves malformed identifiers fail.
func TestBadRequirementIDFormat(t *testing.T) {
	for _, id := range []string{"fr-001", "FR-", "FR-01x", "001", "FR 001"} {
		t.Run("id-"+id, func(t *testing.T) {
			doc := strings.Replace(validYAML, "- id: FR-002", "- id: "+id, 1)
			_, violations, err := LoadFile(writeTemp(t, "project.yaml", doc))
			if err != nil {
				t.Fatalf("bad ID must be a violation: %v", err)
			}
			if len(violations) == 0 {
				t.Fatalf("identifier %q must produce a violation", id)
			}
		})
	}
}

// TestBlankEntries proves whitespace-only values fail wherever content
// is required.
func TestBlankEntries(t *testing.T) {
	doc := validYAML
	doc = strings.Replace(doc, "- maintainer", "- '   '", 1)
	doc = strings.Replace(doc, "- SaaS operation", "- ''", 1)
	doc = strings.Replace(doc, "text: The site builds.", "text: '  '", 1)
	_, violations, err := LoadFile(writeTemp(t, "project.yaml", doc))
	if err != nil {
		t.Fatalf("blank entries must be violations: %v", err)
	}
	if len(violations) < 3 {
		t.Fatalf("expected at least 3 violations, got %v", violations)
	}
}

// TestMalformedFileProvesSyntaxError proves an unparseable file is a
// *SyntaxError (exit code 2 upstream), not a violation list.
func TestMalformedFileProvesSyntaxError(t *testing.T) {
	_, _, err := LoadFile(writeTemp(t, "project.yaml", "name: [unclosed\n"))
	var syn *SyntaxError
	if !errors.As(err, &syn) {
		t.Fatalf("malformed file must yield *SyntaxError, got %v", err)
	}
}

// TestMissingFileProvesSyntaxError proves a missing path is reported
// with its path, not treated as an empty document.
func TestMissingFileProvesSyntaxError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	_, _, err := LoadFile(path)
	var syn *SyntaxError
	if !errors.As(err, &syn) {
		t.Fatalf("missing file must yield *SyntaxError, got %v", err)
	}
	if syn.Path != path {
		t.Fatalf("syntax error must carry the path, got %q", syn.Path)
	}
}

// TestMarshalIsStable proves deterministic serialization: loading and
// re-marshaling is a fixed point, and marshaling is byte-identical
// across runs.
func TestMarshalIsStable(t *testing.T) {
	p, violations, err := LoadFile(writeTemp(t, "project.yaml", validYAML))
	if err != nil || len(violations) != 0 {
		t.Fatalf("fixture must load clean: %v %v", err, violations)
	}
	first, err := Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("marshaling must be byte-identical across runs")
	}
	reloaded, violations, err := LoadBytes(first, "project.yaml")
	if err != nil || len(violations) != 0 {
		t.Fatalf("marshaled output must reload clean: %v %v", err, violations)
	}
	third, err := Marshal(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(third) {
		t.Fatalf("marshal must be a fixed point:\n%s\n%s", first, third)
	}
}

// TestSchemaPatternsMatchCode is the drift guard: the character sets
// enforced in Go must equal the patterns declared in the canonical
// schema, so the two cannot diverge silently.
func TestSchemaPatternsMatchCode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "project.schema.yaml"))
	if err != nil {
		t.Skip("not run from the engine checkout")
	}
	var schema struct {
		Properties struct {
			Name struct {
				Pattern string `yaml:"pattern"`
			} `yaml:"name"`
			Requirements struct {
				Items struct {
					Properties struct {
						ID struct {
							Pattern string `yaml:"pattern"`
						} `yaml:"id"`
					} `yaml:"properties"`
				} `yaml:"items"`
			} `yaml:"requirements"`
		} `yaml:"properties"`
	}
	if err := yaml.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema must parse: %v", err)
	}
	if schema.Properties.Name.Pattern != namePattern.String() {
		t.Fatalf("name pattern drift: schema %q vs code %q",
			schema.Properties.Name.Pattern, namePattern.String())
	}
	got := schema.Properties.Requirements.Items.Properties.ID.Pattern
	if got != requirementIDPattern.String() {
		t.Fatalf("requirement ID pattern drift: schema %q vs code %q",
			got, requirementIDPattern.String())
	}
}
