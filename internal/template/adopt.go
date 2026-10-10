package template

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidateGenerated checks a rendered output or an existing generated
// repository. It deliberately does not apply the canonical template
// source rules that would make a legitimate generated project
// impossible to adopt:
//
//   - engine-leak detection (no *.go, no go.mod) is meaningless here —
//     a generated Go project owns its own source and module file;
//   - the required-file inventory belongs to the template source;
//   - placeholders are legitimate in a rendered-but-not-yet-adopted
//     output, so they are not checked here. Use
//     CheckNoUnresolvedPlaceholders after a full adoption.
//
// What is checked is anything that must never be true of any file
// RepoForge touches: credential-shaped material, hardcoded
// identities, machine-specific absolute paths, unpinned actions, and
// unparseable YAML.
func ValidateGenerated(files []SourceFile) []Issue {
	var issues []Issue
	add := func(path, rule, format string, args ...any) {
		issues = append(issues, Issue{Path: path, Rule: rule, Message: fmt.Sprintf(format, args...)})
	}

	for _, f := range files {
		if unsafeRel(f.Rel) {
			add(f.Rel, "generated/path", "path escapes the target directory")
			continue
		}
		if isBinary(f.Data) {
			continue
		}
		text := string(f.Data)
		for _, re := range secretPatterns {
			if re.MatchString(text) {
				add(f.Rel, "generated/secret", "contains credential-looking material; never write credentials into a repository")
			}
		}
		if m := identityRe.FindString(text); m != "" {
			add(f.Rel, "generated/identity", "contains hardcoded identity %q; ownership values must not be hardcoded (spec.md §8.4)", m)
		}
		if m := absPathRe.FindString(text); m != "" {
			add(f.Rel, "generated/abs-path", "contains machine-specific absolute path %q", m)
		}
		if isYAMLPath(f.Rel) {
			var v any
			if err := yaml.Unmarshal(f.Data, &v); err != nil {
				add(f.Rel, "generated/yaml", "invalid YAML: %v", err)
			}
		}
		if strings.HasPrefix(f.Rel, ".github/workflows/") {
			for _, uses := range usesRe.FindAllStringSubmatch(text, -1) {
				checkPinned(uses[1], func(msg string) { add(f.Rel, "generated/pinning", "%s", msg) })
			}
		}
	}
	return issues
}

func isYAMLPath(rel string) bool {
	if !strings.HasPrefix(rel, ".github/") && !strings.HasPrefix(rel, ".repoforge/") {
		return false
	}
	return strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml")
}

// CheckNoUnresolvedPlaceholders reports every known placeholder that
// survives a full adoption. Adoption requires every known variable, so
// any survivor means the render was incomplete — a repository that
// claims to be adopted while carrying literal placeholder text is worse
// than a failed adoption.
func CheckNoUnresolvedPlaceholders(files []SourceFile) []Issue {
	var issues []Issue
	for _, f := range files {
		if isBinary(f.Data) {
			continue
		}
		for _, m := range placeholderRe.FindAllStringSubmatch(string(f.Data), -1) {
			if isKnownVar(m[1]) {
				issues = append(issues, Issue{
					Path:    f.Rel,
					Rule:    "generated/placeholder",
					Message: fmt.Sprintf("unresolved placeholder {{ .%s }} after adoption", m[1]),
				})
			}
		}
	}
	return issues
}

func isKnownVar(name string) bool {
	for _, v := range KnownVars {
		if v == name {
			return true
		}
	}
	return false
}

func unsafeRel(rel string) bool {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// AdoptWrite is the outcome of publishing an adoption.
type AdoptWrite struct {
	// Written lists the files that were replaced, in the order written.
	Written []string `json:"written"`
}

// ChangedFiles returns the subset of after whose bytes differ from the
// corresponding file in before. The order is after's order (sorted, for
// a deterministically ordered report and write sequence). Files present
// only in after are reported as changed; files present only in before
// are not, because adoption never deletes.
func ChangedFiles(before, after []SourceFile) []SourceFile {
	orig := make(map[string]SourceFile, len(before))
	for _, f := range before {
		orig[f.Rel] = f
	}
	var changed []SourceFile
	for _, f := range after {
		if o, ok := orig[f.Rel]; ok && bytes.Equal(o.Data, f.Data) {
			continue
		}
		changed = append(changed, f)
	}
	return changed
}

// ValidateAdoption checks an adoption before anything is written. It
// validates the generated-repository contract over exactly the files the
// adoption will change — not over the developer's unrelated content,
// which RepoForge does not own — and reports any known placeholder
// that survived rendering in that scope.
func ValidateAdoption(changed []SourceFile) []Issue {
	issues := ValidateGenerated(changed)
	return append(issues, CheckNoUnresolvedPlaceholders(changed)...)
}

// PublishAdoption replaces the changed subset of after over before,
// writing to dir. It computes the full diff first, validates every
// destination, then publishes each file atomically.
//
// Safety contract:
//
//   - Every destination must already exist as a regular file. Symlinks,
//     directories, FIFOs and other special files are refused rather than
//     written through, so adoption can never scribble on a path the
//     repository does not actually own.
//   - Files unchanged by rendering are never touched, so user
//     modifications to other files are preserved.
//   - Each file is published by writing a sibling temporary file and
//     renaming it over the destination, so a reader never observes a
//     half-written file and the executable bit is set explicitly.
//   - If any step fails, every file already published is restored from
//     the in-memory pre-adoption content. Adoption is therefore
//     all-or-nothing: a failure cannot leave a repository silently
//     half-adopted.
//
// An empty diff publishes nothing and succeeds.
func PublishAdoption(dir string, before, after []SourceFile) (AdoptWrite, error) {
	changed := ChangedFiles(before, after)
	if len(changed) == 0 {
		return AdoptWrite{}, nil
	}

	// Validate every destination before writing anything.
	dests := make([]string, len(changed))
	for i, f := range changed {
		if unsafeRel(f.Rel) {
			return AdoptWrite{}, fmt.Errorf("template: refusing to write %s: path escapes the target directory", f.Rel)
		}
		dest := filepath.Join(dir, filepath.FromSlash(f.Rel))
		info, err := os.Lstat(dest)
		if err != nil {
			return AdoptWrite{}, fmt.Errorf("template: %s: %w", f.Rel, err)
		}
		if !info.Mode().IsRegular() {
			return AdoptWrite{}, fmt.Errorf(
				"template: %s is a %s, not a regular file; refusing to overwrite it (remove or replace it with a regular file first)",
				f.Rel, describeMode(info.Mode()))
		}
		dests[i] = dest
	}

	var written []string
	for i, f := range changed {
		if err := writeFileAtomic(dests[i], f.Data, fileMode(f)); err != nil {
			if rbErr := rollback(dests[:len(written)], changed[:len(written)], originalContent(before)); rbErr != nil {
				return AdoptWrite{}, fmt.Errorf("template: writing %s: %v; ALSO FAILED TO ROLL BACK (%v) — restore the listed files from version control before continuing", f.Rel, err, rbErr)
			}
			return AdoptWrite{}, fmt.Errorf("template: writing %s: %w (all changes were rolled back)", f.Rel, err)
		}
		written = append(written, f.Rel)
	}
	return AdoptWrite{Written: written}, nil
}

// rollback restores the pre-adoption content of every already-published
// file. Content is recovered from memory, so rollback works even when the
// filesystem is the thing that failed.
func rollback(dests []string, changed []SourceFile, orig map[string]SourceFile) error {
	var firstErr error
	for i := range changed {
		if i >= len(dests) {
			break
		}
		o, ok := orig[changed[i].Rel]
		if !ok {
			continue
		}
		if err := writeFileAtomic(dests[i], o.Data, fileMode(o)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// writeFileAtomic writes data to a sibling temporary file and renames it
// over dest, so dest is never observably truncated mid-write.
func writeFileAtomic(dest string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".repoforge-adopt-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

func fileMode(f SourceFile) os.FileMode {
	m := f.Mode.Perm()
	if m == 0 {
		return 0o644
	}
	return m
}

func describeMode(m os.FileMode) string {
	switch {
	case m&os.ModeSymlink != 0:
		return "symlink"
	case m.IsDir():
		return "directory"
	case m&os.ModeNamedPipe != 0:
		return "named pipe"
	case m&os.ModeSocket != 0:
		return "socket"
	case m&os.ModeDevice != 0:
		return "device"
	default:
		return "special file"
	}
}

// originalContent indexes before by relative path so a rollback can
// restore pre-adoption bytes without touching the filesystem.
func originalContent(before []SourceFile) map[string]SourceFile {
	m := make(map[string]SourceFile, len(before))
	for _, f := range before {
		m[f.Rel] = f
	}
	return m
}
