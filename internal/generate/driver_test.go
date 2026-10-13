package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner replays canned subprocess outputs in order.
type fakeRunner struct {
	outs  []string
	codes []int
	calls [][]string
}

func (f *fakeRunner) Run(_ context.Context, args []string, _ io.Reader) (int, string, error) {
	f.calls = append(f.calls, args)
	i := len(f.calls) - 1
	if i >= len(f.outs) {
		return 1, "", nil
	}
	code := 0
	if i < len(f.codes) {
		code = f.codes[i]
	}
	return code, f.outs[i], nil
}

func pausedEvent(runID string, payload any) string {
	raw, _ := json.Marshal(map[string]any{
		"event": "paused", "run_id": runID, "payload": payload,
	})
	return string(raw)
}

func doneEvent(report string) string {
	raw, _ := json.Marshal(map[string]any{"event": "done", "report": report})
	return string(raw)
}

func approvePayload(artifact, spec string) map[string]any {
	return map[string]any{
		"action": "approve-spec", "artifact": artifact, "spec": spec,
	}
}

// TestPassArgsCarrySources proves explicit template locations reach
// the journey, so runs work from any directory.
func TestPassArgsCarrySources(t *testing.T) {
	d := &Driver{Workdir: "/w", OutDir: "site",
		ProductSource: "/p/astro", TemplateSource: "/p/template"}
	args := d.passArgs()
	joined := strings.Join(args, " ")
	for _, want := range []string{"--product-source", "/p/astro", "--template-source", "/p/template"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("pass args missing %q: %q", want, joined)
		}
	}
	plain := &Driver{Workdir: "/w", OutDir: "site"}
	if strings.Contains(strings.Join(plain.passArgs(), " "), "product-source") {
		t.Fatal("empty sources must keep the journey defaults")
	}
}

// TestAutoApproveRecordsAttribution proves --yes skips the prompt but
// still records who approved what.
func TestAutoApproveRecordsAttribution(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{
		outs: []string{
			pausedEvent("r1", approvePayload("abc123", "spec text")),
			doneEvent("all good"),
		},
		codes: []int{3, 0},
	}
	prompter := &recordingPrompter{confirm: true}
	d := &Driver{Prompter: prompter, Runner: runner, Out: &bytes.Buffer{},
		Workdir: dir, OutDir: "site", AutoApprove: true,
		codePaused: CodePaused, codeFailed: CodeFailed}
	report, err := d.RunWithAnswers(context.Background(), map[string]any{"project_name": "x"})
	if err != nil {
		t.Fatalf("auto-approved run must succeed: %v", err)
	}
	if report != "all good" {
		t.Fatalf("wrong report: %q", report)
	}
	if prompter.confirms != 0 {
		t.Fatalf("--yes must not prompt, prompted %d times", prompter.confirms)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "payload.json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["artifact"] != "abc123" || payload["by"] == "" || payload["approved"] != true {
		t.Fatalf("approval must bind artifact with attribution: %v", payload)
	}
}

// TestInteractiveApprovalAsksOnce proves the default path prompts and
// records an operator approval.
func TestInteractiveApprovalAsksOnce(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{
		outs: []string{
			pausedEvent("r1", approvePayload("abc123", "spec text")),
			doneEvent("done"),
		},
		codes: []int{3, 0},
	}
	prompter := &recordingPrompter{confirm: true}
	d := &Driver{Prompter: prompter, Runner: runner, Out: &bytes.Buffer{},
		Workdir: dir, OutDir: "site",
		codePaused: CodePaused, codeFailed: CodeFailed}
	if _, err := d.RunWithAnswers(context.Background(), map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if prompter.confirms != 1 {
		t.Fatalf("interactive run must prompt once, got %d", prompter.confirms)
	}
}

type recordingPrompter struct {
	confirm  bool
	confirms int
	answers  map[string]string
}

func (p *recordingPrompter) Ask(q Question) (string, error) {
	if p.answers != nil {
		if a, ok := p.answers[q.Key]; ok {
			return a, nil
		}
	}
	return "answer", nil
}

func (p *recordingPrompter) Confirm(string) (bool, error) {
	p.confirms++
	return p.confirm, nil
}
