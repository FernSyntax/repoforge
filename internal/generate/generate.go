package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/FernSyntax/repoforge/internal/pyruntime"
)

// RunGenerate is the entry point behind `repoforge generate`. It
// collects the brief (interactively or from a JSON file), runs the
// embedded journey, and resolves every pause by asking the operator.
// The final report is returned so callers can print or test it.
func RunGenerate(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer,
	workdir, outDir string, routes []string, briefFile, productSource, templateSource string, autoApprove bool, domain string) (string, error) {
	if strings.TrimSpace(workdir) == "" {
		return "", fmt.Errorf("generate: --dir is required")
	}
	if strings.TrimSpace(outDir) == "" {
		outDir = "site"
	}
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return "", fmt.Errorf("generate: resolving %q: %w", workdir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("generate: creating %q: %w", abs, err)
	}
	if err := ensureDeps(ctx, stdin, stdout); err != nil {
		return "", err
	}

	var answers map[string]any
	if strings.TrimSpace(briefFile) != "" {
		var loadErr error
		answers, loadErr = loadBriefFromFile(briefFile)
		if loadErr != nil {
			return "", fmt.Errorf("generate: loading brief file: %w", loadErr)
		}
	} else {
		prompter := NewTerminalPrompter(stdin, stdout)
		asked, askErr := askBriefInteractive(prompter)
		if askErr != nil {
			return "", askErr
		}
		answers = asked
	}

	prompter := NewTerminalPrompter(stdin, stdout)
	driver := &Driver{
		Prompter: prompter, Runner: &pyruntimeRunner{}, Out: stdout,
		Workdir: abs, OutDir: outDir, Routes: routes,
		ProductSource: productSource, TemplateSource: templateSource,
		AutoApprove: autoApprove, Domain: strings.TrimSpace(domain),
		codePaused: CodePaused, codeFailed: CodeFailed,
	}
	report, err := driver.RunWithAnswers(ctx, answers)
	if err != nil {
		if report != "" {
			fmt.Fprintln(stderr, report)
		}
		return report, err
	}
	fmt.Fprintf(stdout, "\nSite written to %s\n", filepath.Join(abs, outDir))
	return report, nil
}

func loadBriefFromFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func askBriefInteractive(prompter *TerminalPrompter) (map[string]any, error) {
	out := map[string]any{}
	for _, q := range Questions {
		answer, err := prompter.Ask(q)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(answer) != "" {
			out[q.Key] = answer
		}
	}
	return out, nil
}

// ensureDeps makes the runtime usable: extraction, interpreter, and a
// consent-gated dependency install. It caches its work per process.
var (
	ensureOnce sync.Once
	ensureErr  error
)

func ensureDeps(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	ensureOnce.Do(func() {
		dir, err := pyruntime.Extract()
		if err != nil {
			ensureErr = err
			return
		}
		python, err := pyruntime.FindPython()
		if err != nil {
			ensureErr = err
			return
		}
		if err := pyruntime.Deps(pyruntime.PythonPath(dir), dir); err == nil {
			return
		}
		if !consent(stdin, stdout,
			"Install the orchestration dependencies now (pinned, SHA-256 verified)? [y/N] ") {
			ensureErr = fmt.Errorf("dependencies are not installed; re-run and answer y to install them")
			return
		}
		if err := pyruntime.EnsureDeps(context.Background(), python, dir); err != nil {
			ensureErr = err
			return
		}
	})
	return ensureErr
}

func consent(stdin io.Reader, out io.Writer, prompt string) bool {
	f, ok := stdin.(*os.File)
	if ok {
		if info, err := f.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
			fmt.Fprintln(out, "no interactive terminal; declining to install dependencies")
			return false
		}
	}
	fmt.Fprint(out, prompt)
	var answer string
	if _, err := fmt.Fscanln(stdin, &answer); err != nil {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes"
}

// DecodeEvent parses one journey event record; exported for tests.
func DecodeEvent(raw []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &m); err != nil {
		return nil, err
	}
	return m, nil
}
