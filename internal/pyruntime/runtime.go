// Package pyruntime locates a supported Python, materializes the
// embedded orchestration runtime into a version-keyed cache, and runs
// the journey as a subprocess (spec.md §24, FR-036; ADR-0021).
//
// The cache is content-addressed by binary version: a newer engine
// never reuses an older extraction, and a concurrent run of the same
// version reuses it safely (read-only after publication). Dependencies
// are installed from the embedded, hash-pinned requirements file only
// with explicit consent, and only into the user's environment.
package pyruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/FernSyntax/repoforge/internal/version"
)

// MinPythonMajor and MinPythonMinor bound what the embedded runtime
// supports (see orchestration/requirements.in).
const (
	MinPythonMajor = 3
	MinPythonMinor = 11
)

// ErrNoPython reports that no supported interpreter was found.
var ErrNoPython = fmt.Errorf("python %d.%d or newer not found (python.org/download)",
	MinPythonMajor, MinPythonMinor)

// Extract writes the embedded sources into a version-keyed cache
// directory and returns it. It is safe to call concurrently: an
// existing extraction of the same version is reused verbatim.
func Extract() (string, error) {
	dir := cacheDir(version.Version + "-" + runtime.GOOS + "-" + runtime.GOARCH)
	if ready, err := exists(filepath.Join(dir, MainModule)); err != nil {
		return "", err
	} else if ready {
		return dir, nil
	}
	// Ensure parent directory exists before creating temp dir.
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("pyruntime: creating cache parent: %w", err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(dir), "extract-*")
	if err != nil {
		return "", fmt.Errorf("pyruntime: staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	entries, err := listSources()
	if err != nil {
		return "", err
	}
	for _, name := range entries {
		data, err := readSource(name)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(stage, name)), 0o755); err != nil {
			return "", fmt.Errorf("pyruntime: staging %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(stage, name), data, 0o644); err != nil {
			return "", fmt.Errorf("pyruntime: writing %s: %w", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("pyruntime: creating cache: %w", err)
	}
	if err := os.Rename(stage, dir); err != nil {
		// Another run published concurrently: treat as success.
		if _, statErr := os.Stat(dir); statErr == nil {
			return dir, nil
		}
		return "", fmt.Errorf("pyruntime: publishing extraction: %w", err)
	}
	return dir, nil
}

func cacheDir(key string) string {
	base := os.Getenv("REPOFORGE_CACHE_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".cache", "repoforge")
		}
	}
	if base == "" {
		base = filepath.Join(os.TempDir(), "repoforge-cache")
	}
	return filepath.Join(base, "runtime-"+sanitize(key))
}

// sanitize keeps the cache path predictable: a version string only.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

func exists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

// FindPython returns a supported interpreter, preferring explicit
// REPOFFORGE_PYTHON, then python3.13 down to python3.11, then python3.
func FindPython() (string, error) {
	var candidates []string
	if explicit := os.Getenv("REPOFORGE_PYTHON"); explicit != "" {
		candidates = append(candidates, explicit)
	}
	for minor := 13; minor >= MinPythonMinor; minor-- {
		candidates = append(candidates, fmt.Sprintf("python3.%d", minor))
	}
	candidates = append(candidates, "python3", "python")
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		ok, err := supportsRuntime(path)
		if err != nil || !ok {
			continue
		}
		return path, nil
	}
	return "", ErrNoPython
}

func supportsRuntime(path string) (bool, error) {
	cmd := exec.Command(path, "-c",
		"import sys; print('%d.%d' % sys.version_info[:2])")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ".")
	if len(parts) != 2 {
		return false, fmt.Errorf("pyruntime: cannot read %s version", path)
	}
	var major, minor int
	if _, err := fmt.Sscanf(parts[0]+"."+parts[1], "%d.%d", &major, &minor); err != nil {
		return false, err
	}
	if major != MinPythonMajor || minor < MinPythonMinor {
		return false, nil
	}
	return true, nil
}

// Deps reports whether the embedded requirements are importable.
func Deps(python, dir string) error {
	cmd := exec.Command(python, "-c",
		"import langgraph, langgraph.checkpoint.sqlite")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("dependencies missing: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// InstallDeps installs the embedded, hash-pinned requirements into the
// caller's environment. It is only ever called after explicit consent.
func InstallDeps(python, dir string, out io.Writer) error {
	fmt.Fprintln(out, "installing pinned dependencies (verified by SHA-256; one time)")
	cmd := exec.Command(python, "-m", "pip", "install", "--require-hashes",
		"-r", filepath.Join(dir, RequirementsFile))
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installing dependencies: %w", err)
	}
	return nil
}

// Run executes the journey entry point. The caller owns the process's
// stdin, stdout, and stderr, so an interactive TUI can drive it.
func Run(ctx context.Context, python, dir string, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, python, MainModule)
	cmd.Dir = dir
	cmd.Args = append(cmd.Args, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &ExitError{Code: ee.ExitCode(), Err: err}
		}
		return err
	}
	return nil
}

// ExitError carries a non-zero journey exit so callers can branch on
// it (3 = paused, 1 = failed).
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode returns the process exit code, or 1 for a plain error.
func ExitCode(err error) int {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return 1
}
