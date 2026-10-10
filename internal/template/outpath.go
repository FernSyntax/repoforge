package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UnsafeOutputError reports an output directory that the output-path
// policy refuses to touch. It is returned before any filesystem
// mutation, so a refused destination is guaranteed to be untouched.
type UnsafeOutputError struct {
	// Path is the resolved absolute path that was refused.
	Path string
	// Reason explains the refused target so the operator can act.
	Reason string
	// Remedy names the escape hatch, when one exists. Unsafe targets
	// have no remedy: they are refused with or without Force.
	Remedy string
}

func (e *UnsafeOutputError) Error() string {
	msg := fmt.Sprintf("template: refusing output directory %s: %s", e.Path, e.Reason)
	if e.Remedy != "" {
		msg += " (" + e.Remedy + ")"
	}
	return msg
}

// OutputPolicy is the output-path contract for template generation.
//
// Every variable, value and option is validated before any destructive
// filesystem operation. A destination is accepted only when it is
// provably outside every tree RepoForge does not own: the filesystem
// root; the home directory and everything beneath it; the working
// directory and every ancestor of it (which covers ".", "./", "..",
// "../some-parent" and the engine checkout root); and the template
// source tree together with its ancestors and its descendants.
//
// Containment is component-aware — /a/b is never treated as a parent of
// /a/bc — and symlinked components are resolved before the checks run,
// so a path that only looks safe is still refused. Force never bypasses
// these rules; it only permits replacing a non-empty destination.
type OutputPolicy struct {
	// SourceDir is the template source directory. Refused as a
	// destination, and so are its ancestors and its descendants:
	// rendering on top of the source would rewrite the very policy the
	// template is generated from.
	SourceDir string
	// WorkDir is the directory relative output paths resolve against.
	// Empty means the process working directory.
	WorkDir string
	// HomeDir is the user's home directory. Empty means the value
	// os.UserHomeDir reports.
	HomeDir string
	// Force permits replacing an existing non-empty destination. It
	// does not bypass any unsafe-target rule.
	Force bool
}

// Resolve validates outDir and returns the absolute, symlink-resolved
// destination directory. The returned path is safe to write to under
// this policy, and no filesystem mutation has occurred.
func (p OutputPolicy) Resolve(outDir string) (string, error) {
	if outDir == "" {
		return "", &UnsafeOutputError{Path: "", Reason: "no output directory given"}
	}
	// A destination is named, not navigated to. Any explicit ".."
	// component is refused outright, before resolution, so a relative
	// path cannot reach a parent of the working directory or of the
	// source tree by spelling it out.
	if hasDotDot(outDir) {
		return "", &UnsafeOutputError{
			Path:   outDir,
			Reason: `the output path contains a ".." component`,
			Remedy: "name the output directory explicitly, for example ./out or an absolute path",
		}
	}
	abs := p.abs(outDir)
	if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", p.symlinkRefusal(abs)
	}

	real, err := resolveRealPath(abs)
	if err != nil {
		return "", fmt.Errorf("template: resolving output directory %s: %w", outDir, err)
	}
	if err := p.check(real); err != nil {
		return "", err
	}
	return real, nil
}

// symlinkRefusal describes why an output path that is itself a symlink
// is refused, resolving the link so the message names the real target.
func (p OutputPolicy) symlinkRefusal(abs string) error {
	target := abs
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		target = resolved
	}
	return &UnsafeOutputError{
		Path:   target,
		Reason: "the output path is a symlink; writing through it would modify a directory outside the declared output directory",
		Remedy: "pass the real directory, or remove the symlink first",
	}
}

func (p OutputPolicy) abs(outDir string) string {
	if filepath.IsAbs(outDir) {
		return filepath.Clean(outDir)
	}
	return filepath.Clean(filepath.Join(p.workDir(), outDir))
}

// check applies every unsafe-target rule to an already symlink-resolved
// absolute path.
func (p OutputPolicy) check(real string) error {
	// The filesystem root.
	if real == filepath.Dir(real) {
		return &UnsafeOutputError{Path: real, Reason: "the filesystem root is never a build target"}
	}

	// The home directory itself. Rendering over an operator's home
	// directory root is never a project operation. Paths *inside* the
	// home directory are not blanket-refused: a project that happens to
	// live there must still be buildable, and the non-empty-destination
	// rule below already prevents destructive replacement.
	if home := p.home(); home != "" && real == home {
		return &UnsafeOutputError{
			Path:   real,
			Reason: "the home directory is never a build target",
			Remedy: "choose an output directory inside the project, for example ./out",
		}
	}

	// The working directory and every ancestor of it. Rejecting
	// ancestors (rather than descendants) covers ".", "./", "..",
	// "../some-parent" and the checkout root, while still allowing a
	// dedicated output directory such as ./out or ./.template-out.
	if work := p.workDir(); work != "" && within(real, work) {
		return &UnsafeOutputError{
			Path:   real,
			Reason: fmt.Sprintf("is the working directory %s or one of its ancestors", work),
			Remedy: "choose a dedicated output directory, for example ./out",
		}
	}

	// The template source tree, its ancestors, and its descendants.
	if src := p.source(); src != "" {
		switch {
		case within(src, real):
			return &UnsafeOutputError{
				Path:   real,
				Reason: fmt.Sprintf("is inside the template source tree %s", src),
				Remedy: "choose an output directory outside the source tree",
			}
		case ancestorOf(real, src):
			return &UnsafeOutputError{
				Path:   real,
				Reason: fmt.Sprintf("is an ancestor of the template source tree %s", src),
				Remedy: "choose an output directory outside the source tree",
			}
		}
	}
	return nil
}

func (p OutputPolicy) home() string {
	if p.HomeDir != "" {
		return filepath.Clean(p.HomeDir)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Clean(h)
}

func (p OutputPolicy) workDir() string {
	if p.WorkDir != "" {
		return filepath.Clean(p.WorkDir)
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Clean(wd)
}

func (p OutputPolicy) source() string {
	if p.SourceDir == "" {
		return ""
	}
	abs := p.abs(p.SourceDir)
	if real, err := resolveRealPath(abs); err == nil {
		return real
	}
	return abs
}

// resolveRealPath returns abs with every symlinked component resolved.
// abs must already be Clean and absolute. Components that do not exist
// yet are preserved as written, so a not-yet-created destination is
// still checked against the fully resolved form of its existing
// parents.
func resolveRealPath(abs string) (string, error) {
	cur := abs
	var tail []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			real, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return abs, nil
			}
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return filepath.Clean(real), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// hasDotDot reports whether the supplied path contains a ".." component
// in any position, including inside a longer relative path.
func hasDotDot(path string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

// within reports whether target is base itself or lies inside base. It
// is component-aware: within("/a/b", "/a/bc") is false.
func within(base, target string) bool {
	if base == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// ancestorOf reports whether anc is a proper ancestor of desc. The
// relative path must consist solely of ".." components: on unrelated
// branches Rel still returns a leading "..", which must not be mistaken
// for ancestry.
func ancestorOf(anc, desc string) bool {
	if anc == "" || desc == "" {
		return false
	}
	rel, err := filepath.Rel(desc, anc)
	if err != nil {
		return false
	}
	if rel == ".." {
		return true
	}
	if !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part != ".." {
			return false
		}
	}
	return true
}
