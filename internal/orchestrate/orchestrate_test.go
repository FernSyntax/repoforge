package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FernSyntax/repoforge/internal/project"
	"github.com/FernSyntax/repoforge/internal/template"
)

const validDoc = "schemaVersion: v1\nname: demo\npurpose: Demonstration run.\n"

// fakes drives the runner without the engine. failGenerate fails the
// first n generation calls; verifyIssues is returned by Verify.
type fakes struct {
	generateCalls int
	failGenerate  int
	verifyIssues  []template.Issue
}

func (f *fakes) deps() Deps {
	return Deps{
		Validate: func(doc []byte, format string) []project.Violation {
			name := "project.yaml"
			if format == "json" {
				name = "project.json"
			}
			_, violations, err := project.LoadBytes(doc, name)
			if err != nil {
				return []project.Violation{{Field: "", Message: err.Error()}}
			}
			return violations
		},
		Generate: func(ctx context.Context, outDir string, vars map[string]string) error {
			f.generateCalls++
			if f.generateCalls <= f.failGenerate {
				return context.DeadlineExceeded
			}
			return nil
		},
		Verify: func(ctx context.Context, dir string) []template.Issue {
			return f.verifyIssues
		},
	}
}

func newTestRun(t *testing.T, doc string) *State {
	t.Helper()
	s, err := NewRun("run-1", "proj-1", []byte(doc), "yaml", "out", nil, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// drive advances until halted, approving once when asked.
func drive(t *testing.T, s *State, deps Deps) Outcome {
	t.Helper()
	for i := 0; i < 20; i++ {
		outcome, err := s.Advance(context.Background(), deps)
		if err != nil {
			t.Fatalf("advance: %v", err)
		}
		if outcome == OutcomeAwaitingApproval {
			if err := s.GrantApproval(Approval{Action: GenerateAction, Artifact: s.SpecDigest, By: "tester"}); err != nil {
				t.Fatalf("grant: %v", err)
			}
			continue
		}
		if outcome == OutcomeHalted {
			return outcome
		}
	}
	t.Fatal("run did not halt within 20 advances")
	return OutcomeHalted
}

// TestHappyPath proves a valid run reaches done with the generation
// ledger recorded exactly once.
func TestHappyPath(t *testing.T) {
	f := &fakes{}
	s := newTestRun(t, validDoc)
	drive(t, s, f.deps())
	if s.Stage != StageDone {
		t.Fatalf("run must finish done, at %s\n%s", s.Stage, s.Report())
	}
	if f.generateCalls != 1 {
		t.Fatalf("generate must run exactly once, ran %d", f.generateCalls)
	}
	if len(s.CompletedActions) != 1 || s.CompletedActions[0] != "generate:out" {
		t.Fatalf("ledger wrong: %v", s.CompletedActions)
	}
	report := s.Report()
	if !strings.Contains(report, "remaining: none") || !strings.Contains(report, "action required: none") {
		t.Fatalf("completion report wrong:\n%s", report)
	}
}

// TestInvalidSpecFails proves a bad document halts the run as failed
// and resumable, with the cause in the report.
func TestInvalidSpecFails(t *testing.T) {
	f := &fakes{}
	s := newTestRun(t, "schemaVersion: v1\nname: '../x'\npurpose: Demo.\n")
	drive(t, s, f.deps())
	if s.Stage != StageFailed || s.Failure == nil {
		t.Fatalf("invalid spec must fail the run: %s", s.Stage)
	}
	if !s.Failure.Resumable || !strings.Contains(s.Failure.Message, "specification invalid") {
		t.Fatalf("failure record wrong: %+v", s.Failure)
	}
	if !strings.Contains(s.Report(), "resumable: yes") {
		t.Fatalf("report must offer resumption:\n%s", s.Report())
	}
	if f.generateCalls != 0 {
		t.Fatal("generation must never run for an invalid spec")
	}
}

// TestApprovalMismatchRejected proves a stale yes authorizes nothing.
func TestApprovalMismatchRejected(t *testing.T) {
	f := &fakes{}
	s := newTestRun(t, validDoc)
	for i := 0; i < 5; i++ {
		outcome, err := s.Advance(context.Background(), f.deps())
		if err != nil {
			t.Fatal(err)
		}
		if outcome == OutcomeAwaitingApproval {
			break
		}
	}
	if s.Stage != StagePaused || s.Pending == nil {
		t.Fatalf("run must pause for approval: %s", s.Stage)
	}
	if err := s.GrantApproval(Approval{Action: GenerateAction, Artifact: "deadbeef", By: "tester"}); err == nil {
		t.Fatal("approval for other bytes must be rejected")
	}
	if err := s.GrantApproval(Approval{Action: "publish", Artifact: s.SpecDigest, By: "tester"}); err == nil {
		t.Fatal("approval for another action must be rejected")
	}
	if err := s.GrantApproval(Approval{Action: GenerateAction, Artifact: s.SpecDigest, By: ""}); err == nil {
		t.Fatal("anonymous approval must be rejected")
	}
	if s.Stage != StagePaused {
		t.Fatal("rejected approvals must leave the run paused")
	}
}

// TestRepairExhaustion proves a failing generator stops at the budget
// with an honest count.
func TestRepairExhaustion(t *testing.T) {
	f := &fakes{failGenerate: 100}
	s, err := NewRun("run-1", "proj-1", []byte(validDoc), "yaml", "out", nil, Limits{MaxRepairAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, s, f.deps())
	if s.Stage != StageFailed {
		t.Fatalf("exhausted run must fail: %s", s.Stage)
	}
	if s.RepairCount != 3 || !strings.Contains(s.Failure.Message, "repair budget spent") {
		t.Fatalf("budget accounting wrong: %+v", s.Failure)
	}
}

// TestResumeSkipsCompletedActions proves reload-and-advance never
// repeats a recorded external action.
func TestResumeSkipsCompletedActions(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	f1 := &fakes{}
	s := newTestRun(t, validDoc)
	// Advance to Verify: specify, validate, approve(pause+grant), plan, generate.
	for i := 0; i < 10 && s.Stage != StageVerify && s.Stage != StageFailed; i++ {
		outcome, err := s.Advance(context.Background(), f1.deps())
		if err != nil {
			t.Fatal(err)
		}
		if outcome == OutcomeAwaitingApproval {
			if err := s.GrantApproval(Approval{Action: GenerateAction, Artifact: s.SpecDigest, By: "tester"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if s.Stage != StageVerify {
		t.Fatalf("setup must reach verify, at %s", s.Stage)
	}
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("run-1")
	if err != nil {
		t.Fatal(err)
	}
	f2 := &fakes{}
	deps2 := f2.deps()
	// Generation that fires during resume is a defect: fail loudly.
	deps2.Generate = func(ctx context.Context, outDir string, vars map[string]string) error {
		t.Error("resumed run must not re-execute recorded generation")
		return nil
	}
	for i := 0; i < 10 && loaded.Stage != StageDone && loaded.Stage != StageFailed; i++ {
		outcome, err := loaded.Advance(context.Background(), deps2)
		if err != nil {
			t.Fatal(err)
		}
		if outcome == OutcomeHalted {
			break
		}
	}
	if loaded.Stage != StageDone {
		t.Fatalf("resumed run must finish: %s", loaded.Stage)
	}
	if f1.generateCalls != 1 {
		t.Fatalf("generate must have run once total, ran %d", f1.generateCalls)
	}
}

// TestReloadWithoutDocumentFailsHonestly proves a checkpoint saved
// before validation cannot advance without its document.
func TestReloadWithoutDocumentFailsHonestly(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := newTestRun(t, validDoc)
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.Advance(context.Background(), (&fakes{}).deps()); err == nil {
		t.Fatal("reloaded pre-validation state must refuse to advance without its document")
	}
}

// TestBlindRebuildRefused proves repair never regenerates blindly over
// a recorded generation.
func TestBlindRebuildRefused(t *testing.T) {
	f := &fakes{verifyIssues: []template.Issue{{Rule: "x", Message: "broken"}}}
	s := newTestRun(t, validDoc)
	drive(t, s, f.deps())
	if s.Stage != StageFailed {
		t.Fatalf("invalid tree must fail the run: %s", s.Stage)
	}
	if !strings.Contains(s.Failure.Message, "blind rebuild") {
		t.Fatalf("failure must name the refusal: %s", s.Failure.Message)
	}
}

// TestNewRunValidation proves constructor guards: ids are
// filename-safe, the format is closed, and required inputs exist.
func TestNewRunValidation(t *testing.T) {
	if _, err := NewRun("../x", "p", []byte(validDoc), "yaml", "out", nil, Limits{}); err == nil {
		t.Error("traversal run id must be rejected")
	}
	if _, err := NewRun("r", "", []byte(validDoc), "yaml", "out", nil, Limits{}); err == nil {
		t.Error("empty project id must be rejected")
	}
	if _, err := NewRun("r", "p", []byte(validDoc), "toml", "out", nil, Limits{}); err == nil {
		t.Error("unknown format must be rejected")
	}
	if _, err := NewRun("r", "p", nil, "yaml", "out", nil, Limits{}); err == nil {
		t.Error("empty document must be rejected")
	}
	if _, err := NewRun("r", "p", []byte(validDoc), "yaml", "", nil, Limits{}); err == nil {
		t.Error("empty out dir must be rejected")
	}
}

// TestStoreRoundTrip proves save/load fidelity, version gating, and
// traversal-safe ids.
func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := newTestRun(t, validDoc)
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RunID != s.RunID || loaded.Stage != s.Stage || loaded.SpecDigest != s.SpecDigest {
		t.Fatalf("checkpoint must round-trip: %+v", loaded)
	}
	if _, err := store.Load("../x"); err == nil {
		t.Error("traversal run id must be rejected on load")
	}
	if _, err := store.Load("absent"); err == nil {
		t.Error("missing checkpoint must fail")
	}
	// A newer schema version must fail closed, never misread.
	raw, err := os.ReadFile(filepath.Join(dir, "run-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	doctored := strings.Replace(string(raw), `"schemaVersion": "v1"`, `"schemaVersion": "v9"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "run-1.json"), []byte(doctored), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("run-1"); err == nil {
		t.Error("unknown schema version must fail closed")
	}
	if err := store.Delete("run-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("run-1"); err == nil {
		t.Error("deleted checkpoint must be gone")
	}
}

// TestDefaultDepsHappyPath proves the real engine wiring end to end
// against the canonical template source.
func TestDefaultDepsHappyPath(t *testing.T) {
	src, err := filepath.Abs(filepath.Join("..", "..", "template"))
	if err != nil {
		t.Skip("not run from the engine checkout")
	}
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		t.Skip("not run from the engine checkout")
	}
	out := filepath.Join(t.TempDir(), "site")
	s, err := NewRun("run-9", "proj-9", []byte(validDoc), "yaml", out, nil, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	deps := DefaultDeps(src)
	for i := 0; i < 20 && s.Stage != StageDone && s.Stage != StageFailed; i++ {
		outcome, err := s.Advance(context.Background(), deps)
		if err != nil {
			t.Fatal(err)
		}
		if outcome == OutcomeAwaitingApproval {
			if err := s.GrantApproval(Approval{Action: GenerateAction, Artifact: s.SpecDigest, By: "tester"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if s.Stage != StageDone {
		t.Fatalf("real-engine run must finish done, at %s\n%s", s.Stage, s.Report())
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Fatalf("run must generate the tree: %v", err)
	}
}
