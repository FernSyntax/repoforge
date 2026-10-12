package orchestrate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FernSyntax/repoforge/internal/project"
	"github.com/FernSyntax/repoforge/internal/template"
)

// Deps are the runner's external effects, injected so tests drive
// fakes and production wires the engine. Every function is
// deterministic under its inputs; none may consult the network beyond
// what the engine itself does (template verification of development
// builds is local).
type Deps struct {
	// Validate checks the project document; violations fail the run.
	Validate func(doc []byte, format string) []project.Violation
	// Generate renders the tree. It must be safe to skip when its
	// ledger key is already recorded.
	Generate func(ctx context.Context, outDir string, vars map[string]string) error
	// Verify checks the generated tree; issues trigger bounded repair.
	Verify func(ctx context.Context, dir string) []template.Issue
}

func (d Deps) complete() error {
	if d.Validate == nil || d.Generate == nil || d.Verify == nil {
		return fmt.Errorf("orchestrate: run dependencies are incomplete")
	}
	return nil
}

// DefaultDeps wires the real engine: project validation plus template
// generation and verification from a canonical template source.
func DefaultDeps(templateSource string) Deps {
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
			files, err := template.LoadSource(templateSource)
			if err != nil {
				return err
			}
			if issues := template.ValidateSource(files); len(issues) != 0 {
				return fmt.Errorf("orchestrate: template source invalid: %s", issues[0])
			}
			issues, err := template.Build(files, templateSource, outDir, vars, template.BuildOptions{})
			if err != nil {
				return err
			}
			if len(issues) != 0 {
				return fmt.Errorf("orchestrate: generated tree invalid: %s", issues[0])
			}
			return nil
		},
		Verify: func(ctx context.Context, dir string) []template.Issue {
			issues, err := template.Verify(dir, "", false)
			if err != nil {
				return []template.Issue{{Rule: "verify", Message: err.Error()}}
			}
			return issues
		},
	}
}

// Advance performs exactly one stage transition. Fallible stage work
// retries inside the call against the repair budget; exhaustion halts
// the run as failed with a report. Completed external actions are
// never re-executed, so reloading a checkpoint and advancing is safe.
func (s *State) Advance(ctx context.Context, deps Deps) (Outcome, error) {
	if err := deps.complete(); err != nil {
		return OutcomeHalted, err
	}
	switch s.Stage {
	case StageSpecify:
		if len(s.doc) == 0 {
			return OutcomeHalted, fmt.Errorf("orchestrate: run %q has no project document (reloaded checkpoints predate validation cannot advance; start a new run)", s.RunID)
		}
		s.complete(StageSpecify, StageValidate)
		return OutcomeAdvanced, nil
	case StageValidate:
		if violations := deps.Validate(s.doc, s.SpecFormat); len(violations) != 0 {
			s.fail(StageValidate, "specification invalid: "+summarizeViolations(violations), true)
			return OutcomeHalted, nil
		}
		s.complete(StageValidate, StageApprove)
		return OutcomeAdvanced, nil
	case StageApprove:
		if s.Approval != nil && s.Approval.Action == GenerateAction && s.Approval.Artifact == s.SpecDigest {
			s.complete(StageApprove, StagePlan)
			return OutcomeAdvanced, nil
		}
		s.Pending = &ApprovalRequest{Action: GenerateAction, Artifact: s.SpecDigest, Reason: "generation writes the repository tree"}
		s.Stage = StagePaused
		s.UpdatedAt = time.Now().UTC()
		return OutcomeAwaitingApproval, nil
	case StagePlan:
		s.PlanVersion = "v1:" + shortDigest(s.SpecDigest)
		s.complete(StagePlan, StageGenerate)
		return OutcomeAdvanced, nil
	case StageGenerate:
		key := "generate:" + s.OutDir
		if !s.hasAction(key) {
			if err := s.repair(ctx, func() error { return deps.Generate(ctx, s.OutDir, s.Vars) }); err != nil {
				s.fail(StageGenerate, err.Error(), true)
				return OutcomeHalted, nil
			}
			s.CompletedActions = append(s.CompletedActions, key)
		}
		s.complete(StageGenerate, StageVerify)
		return OutcomeAdvanced, nil
	case StageVerify:
		if err := s.repairVerify(ctx, deps); err != nil {
			s.fail(StageVerify, err.Error(), true)
			return OutcomeHalted, nil
		}
		s.complete(StageVerify, StageReview)
		return OutcomeAdvanced, nil
	case StageReview:
		s.complete(StageReview, StageDone)
		return OutcomeAdvanced, nil
	case StageDone, StagePaused, StageFailed:
		return OutcomeHalted, nil
	default:
		return OutcomeHalted, fmt.Errorf("orchestrate: run %q is in an unknown stage %q", s.RunID, s.Stage)
	}
}

// repair runs action until it succeeds or the shared repair budget is
// spent. Every failure counts: the budget bounds the whole run, not
// one stage.
func (s *State) repair(ctx context.Context, action func() error) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancelled: %v", err)
		}
		if err := action(); err == nil {
			return nil
		} else {
			s.RepairCount++
			s.UpdatedAt = time.Now().UTC()
			if s.RepairCount > s.Limits.MaxRepairAttempts {
				return fmt.Errorf("repair budget spent (%d attempts): %v", s.RepairCount, err)
			}
		}
	}
}

// repairVerify loops generate-then-verify until the tree is clean or
// the budget is spent. Regeneration reuses the idempotent ledger: a
// recorded generate is not re-executed, so repair after a recorded
// generate fails closed instead of rebuilding blindly.
func (s *State) repairVerify(ctx context.Context, deps Deps) error {
	key := "generate:" + s.OutDir
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancelled: %v", err)
		}
		if issues := deps.Verify(ctx, s.OutDir); len(issues) == 0 {
			return nil
		} else {
			s.RepairCount++
			s.UpdatedAt = time.Now().UTC()
			if s.RepairCount > s.Limits.MaxRepairAttempts {
				return fmt.Errorf("repair budget spent (%d attempts): tree still invalid: %s", s.RepairCount, summarizeIssues(issues))
			}
			if s.hasAction(key) {
				return fmt.Errorf("tree invalid after recorded generation; refusing blind rebuild: %s", summarizeIssues(issues))
			}
			if err := deps.Generate(ctx, s.OutDir, s.Vars); err != nil {
				// The failed regeneration itself counts: loop back and
				// let the budget check account for it.
				_ = err
				s.RepairCount++
				if s.RepairCount > s.Limits.MaxRepairAttempts {
					return fmt.Errorf("repair budget spent (%d attempts) regenerating: %v", s.RepairCount, err)
				}
				continue
			}
			s.CompletedActions = append(s.CompletedActions, key)
		}
	}
}

func (s *State) hasAction(key string) bool {
	for _, a := range s.CompletedActions {
		if a == key {
			return true
		}
	}
	return false
}

func (s *State) complete(done, next Stage) {
	s.Completed = append(s.Completed, done)
	s.Stage = next
	s.UpdatedAt = time.Now().UTC()
}

func (s *State) fail(stage Stage, message string, resumable bool) {
	s.Stage = StageFailed
	s.Failure = &Failure{Stage: stage, Message: message, Resumable: resumable, Attempts: s.RepairCount}
	s.UpdatedAt = time.Now().UTC()
}

func summarizeViolations(v []project.Violation) string {
	msgs := make([]string, 0, len(v))
	for _, item := range v {
		msgs = append(msgs, item.String())
		if len(msgs) == 3 {
			break
		}
	}
	suffix := ""
	if len(v) > 3 {
		suffix = fmt.Sprintf(" (+%d more)", len(v)-3)
	}
	return strings.Join(msgs, "; ") + suffix
}

func summarizeIssues(issues []template.Issue) string {
	msgs := make([]string, 0, len(issues))
	for _, item := range issues {
		msgs = append(msgs, item.String())
		if len(msgs) == 3 {
			break
		}
	}
	suffix := ""
	if len(issues) > 3 {
		suffix = fmt.Sprintf(" (+%d more)", len(issues)-3)
	}
	return strings.Join(msgs, "; ") + suffix
}
