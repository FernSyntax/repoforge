package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FernSyntax/repoforge/internal/config"
	"github.com/FernSyntax/repoforge/internal/project"
	"github.com/FernSyntax/repoforge/internal/template"
	"github.com/FernSyntax/repoforge/internal/version"
)

// maxDocument caps inline project content: tool arguments carry
// documents, not archives.
const maxDocument = 256 << 10

// tool is one approved engine operation with a precise argument
// contract. Handlers never see unvalidated arguments.
type tool struct {
	name        string
	description string
	schema      map[string]any
	call        func(args map[string]any) (string, bool, error)
}

// registry holds the four approved tools with their directories.
type registry struct {
	root string
	src  string
	// product is the Astro product template source. Empty means the
	// product_build tool is unconfigured and reports so.
	product string
}

func newRegistry(root, src, product string) *registry {
	return &registry{root: root, src: src, product: product}
}

func (r *registry) describe() []any {
	out := make([]any, 0, 5)
	for _, t := range r.all() {
		out = append(out, map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.schema,
		})
	}
	return out
}

func (r *registry) all() []tool {
	return []tool{
		{
			name:        "capabilities",
			description: "Report engine, protocol, and schema versions plus the approved tool names. No arguments.",
			schema:      objSchema(map[string]any{}, nil),
			call: func(args map[string]any) (string, bool, error) {
				return marshalJSON(map[string]any{
					"engine":   engineVersion(),
					"protocol": ProtocolVersion,
					"schemas": []any{
						map[string]any{"name": "repoforge", "version": config.SchemaVersion},
						map[string]any{"name": "project", "version": project.SchemaVersion},
					},
					"tools": []string{"capabilities", "project_validate", "template_build", "template_verify", "product_build"},
				}), false, nil
			},
		},
		{
			name:        "project_validate",
			description: "Validate a project document (FR-031) without writing anything. Returns validity plus violations.",
			schema: objSchema(map[string]any{
				"content": map[string]any{"type": "string", "description": "Project document text."},
				"format":  map[string]any{"type": "string", "enum": []string{"yaml", "json"}, "description": "Document encoding (default yaml)."},
			}, []string{"content"}),
			call: func(args map[string]any) (string, bool, error) {
				content, err := strArg(args, "content", true)
				if err != nil {
					return "", false, err
				}
				if len(content) > maxDocument {
					return "", false, fmt.Errorf("content exceeds %d bytes", maxDocument)
				}
				format := "yaml"
				if f, ok := args["format"]; ok {
					s, ok := f.(string)
					if !ok || (s != "yaml" && s != "json") {
						return "", false, fmt.Errorf("format must be \"yaml\" or \"json\"")
					}
					format = s
				}
				name := "project.yaml"
				if format == "json" {
					name = "project.json"
				}
				_, violations, lerr := project.LoadBytes([]byte(content), name)
				if lerr != nil {
					return marshalJSON(map[string]any{"valid": false, "error": lerr.Error()}), true, nil
				}
				return marshalJSON(map[string]any{"valid": len(violations) == 0, "violations": violations}), len(violations) != 0, nil
			},
		},
		{
			name:        "template_build",
			description: "Render the canonical template source into a directory inside the allowed root. Refuses traversal, absolute paths, and non-empty destinations unless force is set.",
			schema: objSchema(map[string]any{
				"outDir": map[string]any{"type": "string", "description": "Destination relative to the allowed root."},
				"vars":   map[string]any{"type": "object", "description": "Adoption variables (string values)."},
				"force":  map[string]any{"type": "boolean", "description": "Replace a non-empty destination."},
			}, []string{"outDir"}),
			call: func(args map[string]any) (string, bool, error) {
				rel, err := strArg(args, "outDir", true)
				if err != nil {
					return "", false, err
				}
				vars, err := varsArg(args)
				if err != nil {
					return "", false, err
				}
				force, err := boolArg(args, "force")
				if err != nil {
					return "", false, err
				}
				dest, err := resolveWithin(r.root, rel)
				if err != nil {
					return "", false, err
				}
				files, err := template.LoadSource(r.src)
				if err != nil {
					return marshalJSON(map[string]any{"ok": false, "error": err.Error()}), true, nil
				}
				if issues := template.ValidateSource(files); len(issues) != 0 {
					return marshalJSON(map[string]any{"ok": false, "issues": issues}), true, nil
				}
				issues, err := template.Build(files, r.src, dest, vars, template.BuildOptions{Force: force})
				if err != nil {
					return marshalJSON(map[string]any{"ok": false, "issues": issues, "error": err.Error()}), true, nil
				}
				return marshalJSON(map[string]any{"ok": true, "outDir": rel}), false, nil
			},
		},
		{
			name:        "template_verify",
			description: "Verify a built tree inside the allowed root: internal validity plus manifest provenance (development builds verify without source fetch).",
			schema: objSchema(map[string]any{
				"dir": map[string]any{"type": "string", "description": "Built tree relative to the allowed root."},
			}, []string{"dir"}),
			call: func(args map[string]any) (string, bool, error) {
				rel, err := strArg(args, "dir", true)
				if err != nil {
					return "", false, err
				}
				dir, err := resolveWithin(r.root, rel)
				if err != nil {
					return "", false, err
				}
				issues, err := template.Verify(dir, "", false)
				if err != nil {
					return marshalJSON(map[string]any{"ok": false, "issues": issues, "error": err.Error()}), true, nil
				}
				return marshalJSON(map[string]any{"ok": len(issues) == 0, "issues": issues}), len(issues) != 0, nil
			},
		},
		{
			name:        "product_build",
			description: "Render the Astro product source into a directory inside the allowed root. Same contract as template_build with the unresolved-placeholder gate; refuses traversal, absolute paths, and non-empty destinations unless force is set.",
			schema: objSchema(map[string]any{
				"outDir": map[string]any{"type": "string", "description": "Destination relative to the allowed root."},
				"vars":   map[string]any{"type": "object", "description": "Product variables (string values)."},
				"force":  map[string]any{"type": "boolean", "description": "Replace a non-empty destination."},
			}, []string{"outDir"}),
			call: func(args map[string]any) (string, bool, error) {
				if r.product == "" {
					return "", false, fmt.Errorf("product_build is not configured on this server")
				}
				rel, err := strArg(args, "outDir", true)
				if err != nil {
					return "", false, err
				}
				vars, err := varsArg(args)
				if err != nil {
					return "", false, err
				}
				force, err := boolArg(args, "force")
				if err != nil {
					return "", false, err
				}
				dest, err := resolveWithin(r.root, rel)
				if err != nil {
					return "", false, err
				}
				files, err := template.LoadSource(r.product)
				if err != nil {
					return marshalJSON(map[string]any{"ok": false, "error": err.Error()}), true, nil
				}
				issues, err := template.BuildProduct(files, r.product, dest, vars, template.BuildOptions{Force: force})
				if err != nil {
					return marshalJSON(map[string]any{"ok": false, "issues": issues, "error": err.Error()}), true, nil
				}
				return marshalJSON(map[string]any{"ok": true, "outDir": rel}), false, nil
			},
		},
	}
}

// call runs the named tool or reports an unknown one. Argument errors
// are protocol errors; engine failures arrive as isError content so
// the client can display them.
func (r *registry) call(name string, args map[string]any) (string, bool, error) {
	for _, t := range r.all() {
		if t.name == name {
			return t.call(args)
		}
	}
	return "", false, fmt.Errorf("unknown tool %q", name)
}

func engineVersion() string {
	return version.Version
}

func objSchema(props map[string]any, required []string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if required != nil {
		s["required"] = required
	}
	return s
}

// marshalJSON renders tool content. It cannot fail on the shapes above;
// the fallback keeps the contract total.
func marshalJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"error":"encoding result"}`
	}
	return string(raw)
}

func strArg(args map[string]any, name string, required bool) (string, error) {
	v, ok := args[name]
	if !ok || v == nil {
		if required {
			return "", fmt.Errorf("missing required argument %q", name)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	if required && s == "" {
		return "", fmt.Errorf("missing required argument %q", name)
	}
	return s, nil
}

func boolArg(args map[string]any, name string) (bool, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("argument %q must be a boolean", name)
	}
	return b, nil
}

func varsArg(args map[string]any) (map[string]string, error) {
	v, ok := args["vars"]
	if !ok || v == nil {
		return nil, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("argument \"vars\" must be an object of strings")
	}
	out := make(map[string]string, len(obj))
	for k, item := range obj {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("variable %q must be a string", k)
		}
		out[k] = s
	}
	return out, nil
}

// resolveDir canonicalizes a server directory: it must exist as a
// directory, resolved to an absolute symlink-free path.
func resolveDir(path, what string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("mcp: %s directory is required", what)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("mcp: %s directory %q: %w", what, path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("mcp: %s %q is not a directory", what, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("mcp: %s directory %q: %w", what, path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("mcp: %s directory %q: %w", what, path, err)
	}
	return resolved, nil
}

// resolveWithin contains a user-supplied destination inside root.
// Absolute paths are refused outright; symlinks are evaluated from
// the nearest existing ancestor before the containment decision, so
// a symlink inside the root cannot smuggle a target outside it
// (spec.md §17.1).
func resolveWithin(root, user string) (string, error) {
	if user == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(user) {
		return "", fmt.Errorf("path %q must be relative to the allowed root", user)
	}
	joined := filepath.Join(root, user)
	cur := joined
	var trail []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("path %q cannot be resolved", user)
		}
		trail = append([]string{filepath.Base(cur)}, trail...)
		cur = parent
	}
	resolved, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return "", fmt.Errorf("path %q cannot be resolved: %v", user, err)
	}
	full := filepath.Join(append([]string{resolved}, trail...)...)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the allowed root", user)
	}
	return full, nil
}
