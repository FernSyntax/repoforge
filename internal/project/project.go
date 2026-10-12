package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the project model schema version this package
// implements. Documents declaring any other version are rejected.
const SchemaVersion = "v1"

// Requirement is one unit of project scope with a stable identifier.
type Requirement struct {
	ID   string `yaml:"id" json:"id"`
	Text string `yaml:"text" json:"text"`
}

// Project is the validated structured project definition (schema v1).
// The name may seed output directories, so its character set excludes
// anything a filesystem could interpret as navigation.
type Project struct {
	SchemaVersion string        `yaml:"schemaVersion" json:"schemaVersion"`
	Name          string        `yaml:"name" json:"name"`
	Purpose       string        `yaml:"purpose" json:"purpose"`
	Users         []string      `yaml:"users" json:"users"`
	Requirements  []Requirement `yaml:"requirements" json:"requirements"`
	Exclusions    []string      `yaml:"exclusions" json:"exclusions"`
}

// Violation is a single semantic project-model problem.
type Violation struct {
	Field   string
	Message string
}

func (v Violation) String() string {
	if v.Field == "" {
		return v.Message
	}
	return v.Field + ": " + v.Message
}

// SyntaxError reports a project file that cannot be parsed at all.
// Callers treat it as a configuration error (exit code 2).
type SyntaxError struct {
	Path string
	Err  error
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e *SyntaxError) Unwrap() error { return e.Err }

var (
	// namePattern doubles as the traversal guard: it admits no
	// separators, dots, or drive syntax, so a valid name can never
	// name a parent, absolute, or sibling path. It mirrors the schema.
	namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	// requirementIDPattern mirrors the schema: stable identifiers such
	// as FR-001, unique per document.
	requirementIDPattern = regexp.MustCompile(`^[A-Z]+-[0-9]+$`)
)

// LoadFile reads, decodes, and semantically validates a project file.
// The extension selects the encoding (.json, otherwise YAML). A
// malformed file yields a *SyntaxError. Unknown fields and type
// mismatches yield violations (strict decoding, spec.md FR-031).
func LoadFile(path string) (*Project, []Violation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, &SyntaxError{Path: path, Err: err}
	}
	return LoadBytes(data, path)
}

// LoadBytes decodes and validates project content held in memory. The
// name selects the encoding by extension and labels syntax errors.
func LoadBytes(data []byte, name string) (*Project, []Violation, error) {
	path := name
	p := &Project{}
	if filepath.Ext(path) == ".json" {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(p); err != nil {
			var syn *json.SyntaxError
			if errors.As(err, &syn) {
				return nil, nil, &SyntaxError{Path: path, Err: err}
			}
			return nil, []Violation{{Field: "", Message: err.Error()}}, nil
		}
	} else {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(p); err != nil {
			var te *yaml.TypeError
			if errors.As(err, &te) {
				violations := make([]Violation, 0, len(te.Errors))
				for _, msg := range te.Errors {
					violations = append(violations, Violation{Field: "", Message: msg})
				}
				return nil, violations, nil
			}
			return nil, nil, &SyntaxError{Path: path, Err: err}
		}
	}
	return p, Validate(p), nil
}

// Validate performs the semantic checks the schema expresses (plus
// identifier uniqueness, which the schema cannot).
func Validate(p *Project) []Violation {
	var v []Violation
	add := func(field, msg string) {
		v = append(v, Violation{Field: field, Message: msg})
	}
	if p == nil {
		return []Violation{{Field: "", Message: "project document is empty"}}
	}

	if p.SchemaVersion == "" {
		add("schemaVersion", "required field missing")
	} else if p.SchemaVersion != SchemaVersion {
		add("schemaVersion", fmt.Sprintf("unknown schema version %q (this tool supports %q)", p.SchemaVersion, SchemaVersion))
	}
	if p.Name == "" {
		add("name", "required field missing")
	} else if !namePattern.MatchString(p.Name) {
		add("name", "must be lowercase alphanumerics and hyphens")
	}
	if strings.TrimSpace(p.Purpose) == "" {
		add("purpose", "required field missing or blank")
	}

	for i, u := range p.Users {
		if strings.TrimSpace(u) == "" {
			add(fmt.Sprintf("users[%d]", i), "must be non-blank")
		}
	}
	seen := map[string]bool{}
	for i, r := range p.Requirements {
		field := fmt.Sprintf("requirements[%d]", i)
		if r.ID == "" {
			add(field+".id", "required field missing")
		} else if !requirementIDPattern.MatchString(r.ID) {
			add(field+".id", fmt.Sprintf("identifier %q must look like FR-001", r.ID))
		} else if seen[r.ID] {
			add(field+".id", fmt.Sprintf("duplicate requirement identifier %q", r.ID))
		} else {
			seen[r.ID] = true
		}
		if strings.TrimSpace(r.Text) == "" {
			add(field+".text", "required field missing or blank")
		}
	}
	for i, e := range p.Exclusions {
		if strings.TrimSpace(e) == "" {
			add(fmt.Sprintf("exclusions[%d]", i), "must be non-blank")
		}
	}
	return v
}

// Marshal renders the project in its canonical YAML form. Struct field
// order is fixed, so identical projects always serialize to identical
// bytes (spec.md FR-031, deterministic serialization).
func Marshal(p *Project) ([]byte, error) {
	return yaml.Marshal(p)
}
