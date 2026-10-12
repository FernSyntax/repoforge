package pyruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os/exec"
	"sort"
	"strings"

	"github.com/FernSyntax/repoforge/orchestration"
)

// MainModule and RequirementsFile are the embedded entry point and the
// hash-pinned dependency manifest.
const (
	MainModule       = orchestration.MainModule
	RequirementsFile = "requirements.txt"
)

func listSources() ([]string, error) {
	var names []string
	err := fs.WalkDir(orchestration.Sources, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		names = append(names, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

func readSource(name string) ([]byte, error) {
	f, err := orchestration.Sources.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var b strings.Builder
	buf := make([]byte, 32<<10)
	for {
		n, readErr := f.Read(buf)
		b.Write(buf[:n])
		if readErr != nil {
			if readErr.Error() == "EOF" {
				break
			}
			return nil, readErr
		}
	}
	return []byte(b.String()), nil
}

// pythonQuestion is one entry of the embedded question table, parsed
// for the parity check so the TUI cannot drift from what the journey
// asks.
type pythonQuestion struct {
	Key      string `json:"key"`
	Question string `json:"question"`
	Help     string `json:"help"`
}

// ParsePythonQuestions runs the embedded brief module in the target
// interpreter and returns its question table. Using the interpreter
// makes the parity check real: it compares what Python will ask, not a
// transcription of it.
func ParsePythonQuestions(python, sourceDir string) ([]pythonQuestion, error) {
	code := `
import json, sys
sys.path.insert(0, ".")
from brief import QUESTIONS
print(json.dumps([{k: q[k] for k in ("key", "question", "help")} for q in QUESTIONS]))
`
	cmd := exec.Command(python, "-c", code)
	cmd.Dir = sourceDir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running brief module: %w (stderr: %s)", err, errOut.String())
	}
	var qs []pythonQuestion
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &qs); err != nil {
		return nil, err
	}
	return qs, nil
}
