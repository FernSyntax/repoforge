// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

// Package template builds the generated repository baseline consumed by
// the RepoForge template repository (spec.md §13). The source
// material in template/ contains only files intended to appear in a
// generated project — never RepoForge implementation source.
//
// Output is deterministic (spec.md §13.5): identical source and variables
// produce byte-identical output. Placeholders use double-brace
// variable syntax; build renders only the variables provided and
// leaves the rest for adoption-time resolution, and every placeholder
// must be a known variable.
package template

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/FernSyntax/repoforge/internal/brand"
)

// KnownVars is the closed set of template variables. Any placeholder
// outside this set is an error (unintentional placeholder).
var KnownVars = []string{
	"ProjectName",
	"Owner",
	"RepoURL",
	"Description",
	"StandardVersion",
}

// requiredFiles must exist in every valid template source.
var requiredFiles = []string{
	"README.md",
	"LICENSE",
	"CHANGELOG.md",
	"CODE_OF_CONDUCT.md",
	"CONTRIBUTING.md",
	"SECURITY.md",
	"SUPPORT.md",
	"AGENTS.md",
	"DESIGN.md",
	"spec.md",
	".editorconfig",
	".gitattributes",
	".gitignore",
	"scripts/verify",
	".github/CODEOWNERS",
	".github/PULL_REQUEST_TEMPLATE.md",
	".repoforge/config.yml",
	".repoforge/ownership.yml",
	"docs/decisions/README.md",
	"docs/decisions/0000-template.md",
	"docs/wiki/Home.md",
}

// forbiddenNames must never appear in template output (engine
// implementation leakage).
var forbiddenNames = []string{
	"go.mod", "go.sum", "Makefile", "node_modules", "testdata",
}

// forbiddenExtensions must never appear in template output.
var forbiddenExtensions = []string{".go"}

var (
	absPathRe = regexp.MustCompile(`/(home|Users)/[A-Za-z0-9._-]+/|[A-Za-z]:\\Users\\`)
	shaPinRe  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`),
	regexp.MustCompile(`(?:gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}`),
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
}

// identityRe matches email addresses: hardcoded identities (spec.md §8.4, §13.4). Ownership values travel as placeholders, never as
// literal addresses — a generated repository must not name its
// author.
var identityRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// Issue describes one source validation problem.
type Issue struct {
	Path    string `json:"path"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

func (i Issue) String() string {
	if i.Path == "" {
		return fmt.Sprintf("[%s] %s", i.Rule, i.Message)
	}
	return fmt.Sprintf("%s: [%s] %s", i.Path, i.Rule, i.Message)
}

// SourceFile is one file in the template source.
type SourceFile struct {
	Rel  string
	Mode os.FileMode
	Data []byte
}

// LoadSource reads the template source tree in deterministic (sorted)
// order.
func LoadSource(dir string) ([]SourceFile, error) {
	var files []SourceFile
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, SourceFile{
			Rel:  filepath.ToSlash(rel),
			Mode: info.Mode(),
			Data: data,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("template: loading source %s: %w", dir, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	if len(files) == 0 {
		return nil, fmt.Errorf("template: source %s is empty", dir)
	}
	return files, nil
}

// ValidateSource checks the template source against the template model
// (spec.md §13): required files, no engine leakage, no credentials, no
// machine-specific paths, intentional placeholders only, parseable
// YAML, pinned actions.
func ValidateSource(files []SourceFile) []Issue {
	var issues []Issue
	add := func(path, rule, format string, args ...any) {
		issues = append(issues, Issue{Path: path, Rule: rule, Message: fmt.Sprintf(format, args...)})
	}

	present := map[string]SourceFile{}
	for _, f := range files {
		present[f.Rel] = f
	}
	for _, req := range requiredFiles {
		if _, ok := present[req]; !ok {
			add(req, "template/required", "required template file missing")
		}
	}
	if _, ok := present[".github/workflows"]; !ok {
		found := false
		for _, f := range files {
			if strings.HasPrefix(f.Rel, ".github/workflows/") {
				found = true
				break
			}
		}
		if !found {
			add(".github/workflows", "template/required", "at least one workflow file is required")
		}
	}

	known := map[string]bool{}
	for _, v := range KnownVars {
		known[v] = true
	}

	for _, f := range files {
		base := filepath.Base(f.Rel)
		for _, name := range forbiddenNames {
			if base == name || strings.Contains(f.Rel, "/"+name+"/") {
				add(f.Rel, "template/leak", "engine implementation artifact %q must not appear in template output", name)
			}
		}
		for _, ext := range forbiddenExtensions {
			if strings.HasSuffix(f.Rel, ext) {
				add(f.Rel, "template/leak", "engine implementation file extension %q must not appear in template output", ext)
			}
		}
		if isBinary(f.Data) {
			continue
		}
		text := string(f.Data)
		for _, re := range secretPatterns {
			if re.MatchString(text) {
				add(f.Rel, "template/secret", "contains credential-looking material; templates must never carry credentials")
			}
		}
		if m := identityRe.FindString(text); m != "" {
			add(f.Rel, "template/identity", "contains hardcoded identity %q; ownership values must be placeholders (spec.md §13.4)", m)
		}
		for _, msg := range brand.Find(text) {
			add(f.Rel, "template/branding", "%s", msg)
		}
		if m := absPathRe.FindString(text); m != "" {
			add(f.Rel, "template/abs-path", "contains machine-specific absolute path %q", m)
		}
		for _, m := range placeholderRe.FindAllStringSubmatch(text, -1) {
			if !known[m[1]] {
				add(f.Rel, "template/placeholder", "unknown placeholder {{ .%s }} (known: %s)", m[1], strings.Join(KnownVars, ", "))
			}
		}
		if strings.HasPrefix(f.Rel, ".github/") && (strings.HasSuffix(f.Rel, ".yml") || strings.HasSuffix(f.Rel, ".yaml")) {
			var v any
			if err := yaml.Unmarshal(f.Data, &v); err != nil {
				add(f.Rel, "template/yaml", "invalid YAML: %v", err)
			}
		}
		if strings.HasPrefix(f.Rel, ".github/workflows/") {
			for _, uses := range usesRe.FindAllStringSubmatch(text, -1) {
				checkPinned(uses[1], func(msg string) { add(f.Rel, "template/pinning", "%s", msg) })
			}
		}
	}

	if f, ok := present[".repoforge/ownership.yml"]; ok {
		var ownership struct {
			ManagedPaths     []string `yaml:"managed_paths"`
			ScaffoldedPaths  []string `yaml:"scaffolded_paths"`
			UserOwnedPaths   []string `yaml:"user_owned_paths"`
			PlaceholderPaths []string `yaml:"placeholder_paths"`
		}
		if err := yaml.Unmarshal(f.Data, &ownership); err != nil {
			add(f.Rel, "template/yaml", "ownership.yml invalid: %v", err)
		}
	}
	if f, ok := present["scripts/verify"]; ok && f.Mode&0o111 == 0 {
		add(f.Rel, "template/verify", "scripts/verify must be executable")
	}
	return issues
}

var usesRe = regexp.MustCompile(`uses:\s*([^\s#]+)`)

func checkPinned(uses string, add func(string)) {
	if strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "docker://") {
		return
	}
	at := strings.LastIndex(uses, "@")
	if at < 0 {
		add(fmt.Sprintf("action %q is unpinned; pin to a full-length commit SHA", uses))
		return
	}
	if !shaPinRe.MatchString(uses[at+1:]) {
		add(fmt.Sprintf("action %q is not pinned to a full-length commit SHA", uses))
	}
}

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// BuildOptions constrains a build. The zero value refuses to replace a
// non-empty destination; Force opts in explicitly.
type BuildOptions struct {
	// Force permits replacing an existing non-empty destination. It
	// never bypasses the unsafe-target rules in OutputPolicy.
	Force bool
	// Manifest records the provenance written into the generated tree.
	// A build always emits a manifest: a tree without one cannot be
	// traced back to the RepoForge source that produced it. The zero
	// value records an unreleased local build.
	Manifest ManifestOptions
}

// Build renders the source into a destination directory. Provided
// variables are rendered; placeholders for unprovided variables remain
// (adoption-time resolution).
//
// Safety contract (spec.md §5.3, §10.5, P7): every variable and every option
// is validated, the destination is resolved through symlinks and
// checked against the output-path policy, and the rendered output is
// validated — all before anything is deleted. Generation happens in a
// temporary directory that is published into place by rename, so a
// failure during rendering or validation leaves the destination exactly
// as it was, and a failure during publication restores it.
//
// A non-empty pre-existing destination is refused unless Force is set.
func Build(files []SourceFile, srcDir, outDir string, vars map[string]string, opts BuildOptions) ([]Issue, error) {
	if err := ValidateVars(vars); err != nil {
		return nil, err
	}
	dest, err := OutputPolicy{SourceDir: srcDir, Force: opts.Force}.Resolve(outDir)
	if err != nil {
		return nil, err
	}

	rendered := RenderFiles(files, vars)
	manifest := BuildManifest(rendered, opts.Manifest)
	rendered = append(rendered, SourceFile{Rel: ManifestFileName, Data: nil, Mode: 0o644})
	if issues := ValidateBuiltTree(rendered); len(issues) > 0 {
		return issues, fmt.Errorf("template: rendered output failed validation (%d issue(s)); %s was not modified", len(issues), dest)
	}
	if err := checkDestination(dest, opts.Force); err != nil {
		return nil, err
	}
	if err := publishTree(dest, rendered, manifest); err != nil {
		return nil, err
	}
	return nil, nil
}

// checkDestination rejects a destination that cannot be safely
// replaced. It never mutates anything.
func checkDestination(dest string, force bool) error {
	info, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("template: inspecting output directory %s: %w", dest, err)
	}
	if !info.IsDir() {
		return &UnsafeOutputError{
			Path:   dest,
			Reason: "exists and is not a directory; refusing to replace it",
			Remedy: "point --out at a directory, or pass --force to replace it",
		}
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return fmt.Errorf("template: inspecting output directory %s: %w", dest, err)
	}
	if len(entries) == 0 {
		return nil
	}
	if !force {
		return &UnsafeOutputError{
			Path:   dest,
			Reason: fmt.Sprintf("already exists and is not empty (%d entries); refusing to delete pre-existing files", len(entries)),
			Remedy: "pass --force to replace it, or choose an empty directory",
		}
	}
	return nil
}

// publishTree renders the whole tree into a temporary directory beside
// dest and then swaps it into place. dest is only ever replaced by a
// fully written tree.
func publishTree(dest string, files []SourceFile, manifest *Manifest) error {
	parent := filepath.Dir(dest)
	if _, err := os.Stat(parent); err != nil {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("template: creating output parent directory: %w", err)
		}
	}
	stage, err := os.MkdirTemp(parent, ".repoforge-build-*")
	if err != nil {
		return fmt.Errorf("template: staging build: %w", err)
	}
	defer os.RemoveAll(stage)

	if err := writeTree(stage, files); err != nil {
		return err
	}
	// The manifest is written last so it can enumerate every other
	// file's digest, and it is regenerated from the staged tree so a
	// rebuild from the same inputs is byte-identical.
	staged, err := LoadSource(stage)
	if err != nil {
		return fmt.Errorf("template: reading staged tree: %w", err)
	}
	manifest = BuildManifest(staged, manifestOptionsOf(manifest))
	if err := WriteManifest(stage, manifest); err != nil {
		return err
	}
	// Validate the staged tree again now that the manifest is real: the
	// destination is untouched, so a bad manifest aborts the publish
	// instead of shipping an untraceable tree.
	staged, err = LoadSource(stage)
	if err != nil {
		return fmt.Errorf("template: reading staged tree: %w", err)
	}
	if issues := ValidateBuiltTree(staged); len(issues) > 0 {
		return fmt.Errorf("template: staged output failed validation (%d issue(s)); %s was not modified: %s",
			len(issues), dest, issues[0])
	}
	if err := swapIntoPlace(dest, stage); err != nil {
		return err
	}
	return nil
}

// manifestOptionsOf restores the provenance of an already-built
// manifest. Only the recording fields are reused; the digest and file
// list are recomputed from the staged tree.
func manifestOptionsOf(m *Manifest) ManifestOptions {
	if m == nil {
		return ManifestOptions{}
	}
	return ManifestOptions{
		RepoForgeVersion: m.RepoForgeVersion,
		SourceRef:        m.SourceRef,
		SourceCommit:     m.SourceCommit,
		SourceURL:        m.SourceURL,
	}
}

func writeTree(root string, files []SourceFile) error {
	for _, f := range files {
		dest := filepath.Join(root, filepath.FromSlash(f.Rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("template: creating directory for %s: %w", f.Rel, err)
		}
		if err := os.WriteFile(dest, f.Data, fileMode(f)); err != nil {
			return fmt.Errorf("template: writing %s: %w", f.Rel, err)
		}
	}
	return nil
}

// swapIntoPlace replaces dest with the staged tree. If the swap fails
// after the old content has been moved aside, the old content is moved
// back, so a failed publication is not a lost one.
func swapIntoPlace(dest, stage string) error {
	if _, err := os.Lstat(dest); err != nil {
		if os.IsNotExist(err) {
			return os.Rename(stage, dest)
		}
		return err
	}
	parent := filepath.Dir(dest)
	backup, err := os.MkdirTemp(parent, ".repoforge-backup-*")
	if err != nil {
		return fmt.Errorf("template: reserving backup name: %w", err)
	}
	backupName := backup
	// The reservation is an empty directory; the name is what matters.
	_ = os.Remove(backupName)
	defer os.RemoveAll(backupName)

	if err := os.Rename(dest, backupName); err != nil {
		return fmt.Errorf("template: moving previous output aside: %w", err)
	}
	if err := os.Rename(stage, dest); err != nil {
		if rbErr := os.Rename(backupName, dest); rbErr != nil {
			return fmt.Errorf("template: publishing output: %v; restoring the previous output also failed: %v — restore from version control before continuing", err, rbErr)
		}
		return fmt.Errorf("template: publishing output: %w", err)
	}
	return nil
}

// AdoptReport describes the substitutions applied to one file. The
// counts are reported per variable, in KnownVars order, by Describe.
type AdoptReport struct {
	Rel           string         `json:"path"`
	Substitutions map[string]int `json:"substitutions"`
}

// Adopt renders every known variable in files, for adopting a
// generated repository in place. All known variables are required:
// partial adoption is what left repositories half-templated, so
// missing variables fail instead of silently leaving placeholders.
// Unknown variable keys and invalid values are rejected (closed set,
// like Build).
//
// Binary files are never touched. Rendering copies data — inputs are
// never modified — and the returned files carry the results. Nothing
// is written; callers publish with PublishAdoption.
func Adopt(files []SourceFile, vars map[string]string) ([]SourceFile, []AdoptReport, error) {
	if err := requireAllVars(vars); err != nil {
		return nil, nil, err
	}
	if err := ValidateVars(vars); err != nil {
		return nil, nil, err
	}
	rendered := make([]SourceFile, len(files))
	var report []AdoptReport
	for i, f := range files {
		rendered[i] = f
		if isBinary(f.Data) {
			continue
		}
		text, counts := Render(string(f.Data), vars)
		if len(counts) == 0 {
			continue
		}
		data := make([]byte, len(text))
		copy(data, text)
		rendered[i].Data = data
		report = append(report, AdoptReport{Rel: f.Rel, Substitutions: map[string]int(counts)})
	}

	// Adoption also releases the provenance gate. Without this the
	// manifest would keep recording the engine source commit that built
	// the repository, and the adopting project's own CI would fail it
	// for every later change — including the application source and the
	// go.mod the project exists to carry.
	if out, manifestReport := adoptManifest(rendered); manifestReport != nil {
		rendered = out
		report = append(report, *manifestReport)
	}
	return rendered, report, nil
}

// adoptManifest rewrites the built manifest, when the tree carries one,
// into its adopted form, so PublishAdoption writes it atomically with
// every other changed file. A malformed manifest is left alone for
// validation to report rather than being silently repaired.
func adoptManifest(files []SourceFile) ([]SourceFile, *AdoptReport) {
	idx := -1
	for i, f := range files {
		if f.Rel == ManifestFileName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return files, nil
	}
	var m Manifest
	if err := strictJSON(files[idx].Data, &m); err != nil {
		return files, nil
	}
	adopted := AdoptManifest(&m)
	data, err := json.MarshalIndent(adopted, "", "  ")
	if err != nil {
		return files, nil
	}
	out := append([]SourceFile(nil), files...)
	out[idx].Data = append(data, '\n')
	return out, &AdoptReport{
		Rel:           ManifestFileName,
		Substitutions: map[string]int{"adopted": 1},
	}
}

// requireAllVars enforces the completeness rule: adoption with a
// missing variable is the failure mode adoption exists to prevent.
func requireAllVars(vars map[string]string) error {
	var missing []string
	for _, v := range KnownVars {
		if _, ok := vars[v]; !ok {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("template: missing variable %s (all required: %s)",
			quoteAll(missing), strings.Join(KnownVars, ", "))
	}
	return nil
}

func quoteAll(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(q, ", ")
}

// AdoptableTarget verifies dir is safe to adopt in place: it must carry
// a generated policy as a regular file, and it must not live inside the
// engine checkout (adopting there would rewrite template sources —
// verified the hard way). Ancestor check, so relative paths into the
// engine tree are refused too, and symlinks are resolved first so a
// link into the engine tree cannot disguise the target.
func AdoptableTarget(dir string) error {
	if dir == "" {
		return fmt.Errorf("template: no target directory given")
	}
	real, err := resolveRealPath(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("template: resolving target %s: %w", dir, err)
	}
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("template: refusing to adopt through the symlink %s (it resolves to %s); pass the real directory", dir, real)
	}
	policy := filepath.Join(real, ".repoforge", "config.yml")
	info, err := os.Stat(policy)
	if err != nil {
		return fmt.Errorf("template: %s has no .repoforge/config.yml (not a generated repository)", dir)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("template: %s/.repoforge/config.yml is not a regular file; refusing to adopt", dir)
	}
	if insideEngineTree(real) {
		return fmt.Errorf("template: refusing to adopt inside the engine checkout (would rewrite template sources)")
	}
	return nil
}

// insideEngineTree reports whether dir is or is nested under a
// directory containing both go.mod and internal/template — the
// engine layout, which no generated repository has.
func insideEngineTree(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			if st, err := os.Stat(filepath.Join(abs, "internal", "template")); err == nil && st.IsDir() {
				return true
			}
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return false
		}
		abs = parent
	}
}
