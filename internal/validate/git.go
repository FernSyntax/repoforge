package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// findRoot walks upward from start looking for a .git entry. The result
// is absolute and symlink-resolved, so findings are stable regardless of
// where inside the repository the command was invoked.
func findRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fatal("cannot resolve start directory %q: %v", start, err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fatal("cannot resolve start directory %q: %v", start, err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", fatal("start directory %q is not a directory", start)
	}
	dir := abs
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fatal("%s is not inside a git repository (no .git found walking up from %s)", start, abs)
		}
		dir = parent
	}
}

// requireGit verifies the git executable is available. Repository data
// (tracked files, branch, commits) is read through the git binary so
// validation reflects exactly what git sees.
func requireGit() error {
	if _, err := exec.LookPath("git"); err != nil {
		return fatal("git executable not found in PATH")
	}
	return nil
}

// gitOutput runs a read-only git command in dir and returns trimmed
// stdout. Commands run with LC_ALL=C so messages and ordering are
// locale-independent (spec.md §12.2).
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// trackedFiles lists files tracked by git (committed state), in git's
// deterministic order.
func trackedFiles(root string) ([]string, error) {
	out, err := gitOutput(root, "ls-files")
	if err != nil {
		return nil, fatal("cannot list tracked files: %v", err)
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// rel renders path relative to root for stable findings output.
func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}
