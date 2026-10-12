package generate

import (
	"bytes"
	"context"
	"io"

	"github.com/FernSyntax/repoforge/internal/pyruntime"
)

// pyruntimeRunner runs the journey subprocess against the embedded
// runtime, extracting sources and finding an interpreter first. The
// heavy setup lives here so the driver stays a pure loop.
type pyruntimeRunner struct {
	python string
	dir    string
}

func (r *pyruntimeRunner) Run(ctx context.Context, args []string, stdin io.Reader) (int, string, error) {
	python, dir, err := r.ensure()
	if err != nil {
		return 1, "", err
	}
	var out, errOut bytes.Buffer
	if err := pyruntime.Run(ctx, python, dir, stdin, &out, &errOut, args...); err != nil {
		return pyruntime.ExitCode(err), out.String(), err
	}
	return 0, out.String(), nil
}

func (r *pyruntimeRunner) ensure() (string, string, error) {
	if r.python != "" {
		return r.python, r.dir, nil
	}
	dir, err := pyruntime.Extract()
	if err != nil {
		return "", "", err
	}
	systemPython, err := pyruntime.FindPython()
	if err != nil {
		return "", "", err
	}
	// Create venv using system python, then use venv python
	if err := pyruntime.EnsureDeps(context.Background(), systemPython, dir); err != nil {
		return "", "", err
	}
	venvPython := pyruntime.PythonPath(dir)
	r.python, r.dir = venvPython, dir
	return venvPython, dir, nil
}
