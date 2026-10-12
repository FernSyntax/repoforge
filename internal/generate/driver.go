package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Prompter supplies the driver's input. Tests inject fakes; production
// uses TerminalPrompter.
type Prompter interface {
	// Ask poses one question and returns the answer.
	Ask(q Question) (string, error)
	// Confirm poses a yes/no. Non-interactive implementations must
	// return false rather than invent consent (spec.md P7).
	Confirm(prompt string) (bool, error)
}

// Runner runs the journey subprocess. Tests inject a fake; production
// uses pyruntime.
type Runner interface {
	// Run executes the journey and returns its exit code. The returned
	// stdout is the journey's JSON event stream.
	Run(ctx context.Context, args []string, stdin io.Reader) (code int, stdout string, err error)
}

// Driver ties the pieces together.
type Driver struct {
	Prompter Prompter
	Runner   Runner
	Out      io.Writer
	Workdir  string
	OutDir   string
	Routes   []string

	codePaused int
	codeFailed int
}

// Pause codes the journey uses. Exit 3 means "resume me".
const (
	CodePaused = 3
	CodeFailed = 1
)

// New builds a driver around the production runner.
func New(p Prompter, out io.Writer, workdir, outDir string, routes []string) *Driver {
	return &Driver{
		Prompter: p, Runner: &pyruntimeRunner{}, Out: out,
		Workdir: workdir, OutDir: outDir, Routes: routes,
		codePaused: CodePaused, codeFailed: CodeFailed,
	}
}

type event struct {
	Event   string          `json:"event"`
	RunID   string          `json:"run_id"`
	Payload json.RawMessage `json:"payload"`
	Report  string          `json:"report"`
}

type pausedPayload struct {
	Questions []struct {
		Key      string `json:"key"`
		Question string `json:"question"`
		Help     string `json:"help"`
	} `json:"questions"`
	Action   string `json:"action"`
	Artifact string `json:"artifact"`
	Spec     string `json:"spec"`
}

// Run drives the journey to a terminal state: ask, approve, resume,
// report. It returns the final report, or an error carrying the
// failure's first line.
func (d *Driver) Run(ctx context.Context) (string, error) {
	answers, err := d.askBrief()
	if err != nil {
		return "", err
	}
	if err := writeJSON(filepath.Join(d.Workdir, "brief.json"), answers); err != nil {
		return "", err
	}
	code, out, err := d.Runner.Run(ctx, d.passArgs(), nil)
	if err != nil && code != d.codePaused && code != d.codeFailed {
		return "", err
	}
	for i := 0; i < 12; i++ {
		ev, parseErr := parseEvent(out)
		if parseErr != nil {
			return "", fmt.Errorf("generate: reading journey output: %w", parseErr)
		}
		switch ev.Event {
		case "done":
			fmt.Fprintln(d.Out, ev.Report)
			return ev.Report, nil
		case "failed":
			return ev.Report, fmt.Errorf("generate: %s", firstLine(ev.Report))
		case "paused":
			resume, herr := d.handlePause(ev.Payload)
			if herr != nil {
				return "", herr
			}
			if resume == nil {
				return "", fmt.Errorf("generate: stopped before approval")
			}
			payloadFile, ferr := writeJSONN(d.Workdir, "payload.json", resume)
			if ferr != nil {
				return "", ferr
			}
			code, out, err = d.Runner.Run(ctx,
				d.resumeArgs(ev.RunID, payloadFile), nil)
			if err != nil && code != d.codePaused && code != d.codeFailed {
				return "", err
			}
		default:
			return "", fmt.Errorf("generate: unexpected journey event %q", ev.Event)
		}
	}
	return "", fmt.Errorf("generate: journey did not settle (bound reached)")
}

func (d *Driver) passArgs() []string {
	args := []string{"start", "--workdir", d.Workdir, "--site-rel", d.OutDir,
		"--json", "--brief", filepath.Join(d.Workdir, "brief.json")}
	return append(args, routeArgs(d.Routes)...)
}

func (d *Driver) resumeArgs(thread, payloadFile string) []string {
	args := []string{"resume", "--workdir", d.Workdir, "--site-rel", d.OutDir,
		"--json", "--thread", thread, "--payload", payloadFile}
	return append(args, routeArgs(d.Routes)...)
}

func routeArgs(routes []string) []string {
	var args []string
	for _, r := range routes {
		args = append(args, "--route", r)
	}
	return args
}

// askBrief prompts for every intake answer.
func (d *Driver) askBrief() (map[string]any, error) {
	out := map[string]any{}
	for _, q := range Questions {
		answer, err := d.Prompter.Ask(q)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(answer) != "" {
			out[q.Key] = answer
		}
	}
	return out, nil
}

// handlePause renders the pause request and returns the resume payload,
// or nil when the operator declines.
func (d *Driver) handlePause(raw json.RawMessage) (map[string]any, error) {
	var p pausedPayload
	if len(raw) == 0 {
		return nil, fmt.Errorf("generate: empty pause payload")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("generate: reading pause payload: %w", err)
	}
	if len(p.Questions) > 0 {
		fmt.Fprintln(d.Out, "The journey needs more detail before it can continue:")
		answers := map[string]any{}
		for _, q := range p.Questions {
			answer, err := d.Prompter.Ask(Question{Key: q.Key, Question: q.Question, Help: q.Help})
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(answer) != "" {
				answers[q.Key] = answer
			}
		}
		if len(answers) == 0 {
			return nil, fmt.Errorf("generate: no answers supplied")
		}
		return answers, nil
	}
	if p.Action == "approve-spec" {
		fmt.Fprintln(d.Out, strings.Repeat("─", 64))
		fmt.Fprintln(d.Out, p.Spec)
		fmt.Fprintln(d.Out, strings.Repeat("─", 64))
		fmt.Fprintf(d.Out, "Approve this specification for digest %s? ", shortDigest(p.Artifact))
		ok, err := d.Prompter.Confirm("yes")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return map[string]any{"approved": true, "artifact": p.Artifact}, nil
	}
	return nil, fmt.Errorf("generate: unknown pause request %q", p.Action)
}

func shortDigest(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func parseEvent(out string) (event, error) {
	var ev event
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		if err := dec.Decode(&ev); err != nil {
			if err == io.EOF {
				break
			}
			return ev, err
		}
		if ev.Event != "" {
			return ev, nil
		}
	}
	return ev, fmt.Errorf("no event in output")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func writeJSON(path string, v any) error {
	_, err := writeJSONN(filepath.Dir(path), filepath.Base(path), v)
	return err
}

func writeJSONN(dir, name string, v any) (string, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
