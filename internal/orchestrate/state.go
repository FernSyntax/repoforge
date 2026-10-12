package orchestrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// StateVersion versions the run-state contract. Loaders reject unknown
// versions so a v2 can supersede this shape without silent
// misinterpretation.
const StateVersion = "v1"

// Stage is one explicit workflow position. Done, Paused, and Failed
// are halted: Advance leaves them in place and reports why.
type Stage string

// Workflow stages in transition order.
const (
	StageSpecify  Stage = "specify"
	StageValidate Stage = "validate"
	StageApprove  Stage = "approve"
	StagePlan     Stage = "plan"
	StageGenerate Stage = "generate"
	StageVerify   Stage = "verify"
	StageReview   Stage = "review"
	StageDone     Stage = "done"
	StagePaused   Stage = "paused"
	StageFailed   Stage = "failed"
)

// Outcome classifies one Advance call.
type Outcome string

const (
	// OutcomeAdvanced means the run moved to a new stage.
	OutcomeAdvanced Outcome = "advanced"
	// OutcomeAwaitingApproval means the run paused for a human
	// decision; GrantApproval resumes it.
	OutcomeAwaitingApproval Outcome = "awaiting-approval"
	// OutcomeHalted means the run is done or failed; see Report.
	OutcomeHalted Outcome = "halted"
)

// Limits bounds every autonomous loop. Zero values are replaced by
// DefaultLimits in NewRun.
type Limits struct {
	MaxRepairAttempts int
}

// DefaultLimits is the v0.2.0 autonomy budget.
func DefaultLimits() Limits {
	return Limits{MaxRepairAttempts: 3}
}

// Approval binds a human decision to one action on one artifact. An
// approval names the specification digest it was granted for: editing
// the document voids it.
type Approval struct {
	Action string `json:"action"`
	// Artifact is the specification digest the decision covers.
	Artifact string    `json:"artifact"`
	By       string    `json:"by"`
	At       time.Time `json:"at"`
}

// ApprovalRequest is a paused run asking for a decision.
type ApprovalRequest struct {
	Action   string `json:"action"`
	Artifact string `json:"artifact"`
	Reason   string `json:"reason"`
}

// Failure is a halted run's terminal record.
type Failure struct {
	Stage     Stage  `json:"stage"`
	Message   string `json:"message"`
	Resumable bool   `json:"resumable"`
	Attempts  int    `json:"attempts"`
}

// State is the complete resumable run. It carries references and
// digests, never generated file contents and never credentials.
type State struct {
	SchemaVersion string            `json:"schemaVersion"`
	RunID         string            `json:"runId"`
	ProjectID     string            `json:"projectId"`
	Stage         Stage             `json:"stage"`
	SpecDigest    string            `json:"specDigest"`
	SpecFormat    string            `json:"specFormat"`
	OutDir        string            `json:"outDir"`
	Vars          map[string]string `json:"vars"`
	PlanVersion   string            `json:"planVersion"`
	Completed     []Stage           `json:"completed"`
	// CompletedActions ledgers external effects by stable key
	// ("generate:<outDir>"); a key present here is never re-executed.
	CompletedActions []string         `json:"completedActions"`
	RepairCount      int              `json:"repairCount"`
	Limits           Limits           `json:"limits"`
	Pending          *ApprovalRequest `json:"pending,omitempty"`
	Approval         *Approval        `json:"approval,omitempty"`
	Failure          *Failure         `json:"failure,omitempty"`
	UpdatedAt        time.Time        `json:"updatedAt"`
	// doc is the in-memory project document. Unexported, so checkpoints
	// stay small and carry digests instead of contents; a reloaded run
	// replays only stages that need no document.
	doc []byte `json:"-"`
}

// GenerateAction is the consequential action gated by approval: it
// writes the generated tree.
const GenerateAction = "generate"

// NewRun starts a run over a project document. The document bytes are
// digested, not stored: checkpoints stay small and approvals bind to
// the exact bytes reviewed.
func NewRun(runID, projectID string, doc []byte, format, outDir string, vars map[string]string, limits Limits) (*State, error) {
	if !validID(runID) {
		return nil, fmt.Errorf("orchestrate: invalid run id %q", runID)
	}
	if !validID(projectID) {
		return nil, fmt.Errorf("orchestrate: invalid project id %q", projectID)
	}
	if format != "yaml" && format != "json" {
		return nil, fmt.Errorf("orchestrate: format must be \"yaml\" or \"json\"")
	}
	if len(doc) == 0 {
		return nil, fmt.Errorf("orchestrate: project document is empty")
	}
	if outDir == "" {
		return nil, fmt.Errorf("orchestrate: output directory is required")
	}
	if limits.MaxRepairAttempts <= 0 {
		limits = DefaultLimits()
	}
	sum := sha256.Sum256(doc)
	return &State{
		SchemaVersion: StateVersion,
		RunID:         runID,
		ProjectID:     projectID,
		Stage:         StageSpecify,
		SpecDigest:    hex.EncodeToString(sum[:]),
		SpecFormat:    format,
		OutDir:        outDir,
		Vars:          vars,
		Limits:        limits,
		UpdatedAt:     time.Now().UTC(),
		doc:           append([]byte(nil), doc...),
	}, nil
}

// validID doubles as the checkpoint-filename guard: run and project
// ids can never name a parent, absolute, or sibling path.
func validID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return s[0] != '-'
}

// GrantApproval records a human decision for the pending request. The
// action and artifact must match exactly: a stale "yes" for other
// bytes authorizes nothing.
func (s *State) GrantApproval(a Approval) error {
	if s.Stage != StagePaused || s.Pending == nil {
		return fmt.Errorf("orchestrate: run %q is not awaiting approval", s.RunID)
	}
	if a.Action != s.Pending.Action || a.Artifact != s.Pending.Artifact {
		return fmt.Errorf("orchestrate: approval does not match pending %s on %s", s.Pending.Action, shortDigest(s.Pending.Artifact))
	}
	if strings.TrimSpace(a.By) == "" {
		return fmt.Errorf("orchestrate: approval must name who granted it")
	}
	a.At = time.Now().UTC()
	s.Approval = &a
	s.Pending = nil
	s.Stage = StageApprove
	s.UpdatedAt = a.At
	return nil
}

func shortDigest(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}

// Report explains the run in the failure-report shape: what failed,
// which stage, whether files changed, whether it can resume, what was
// verified, what remains, and what the operator must do.
func (s *State) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "run %s (project %s): stage %s\n", s.RunID, s.ProjectID, s.Stage)
	fmt.Fprintf(&b, "spec %s (%s)\n", shortDigest(s.SpecDigest), s.SpecFormat)
	if len(s.CompletedActions) == 0 {
		b.WriteString("files changed: none\n")
	} else {
		fmt.Fprintf(&b, "files changed: %s\n", strings.Join(s.CompletedActions, ", "))
	}
	switch s.Stage {
	case StageDone:
		b.WriteString("verified: specification valid, tree generated and verified\n")
		b.WriteString("remaining: none\n")
		b.WriteString("action required: none\n")
	case StagePaused:
		b.WriteString("verified: specification valid\n")
		b.WriteString("remaining: approval for " + s.Pending.Action + " on spec " + shortDigest(s.Pending.Artifact) + " (" + s.Pending.Reason + ")\n")
		b.WriteString("resumable: yes, via GrantApproval then Advance\n")
		b.WriteString("action required: approve or reject " + s.Pending.Action + "\n")
	case StageFailed:
		fmt.Fprintf(&b, "failed at stage %s: %s\n", s.Failure.Stage, s.Failure.Message)
		fmt.Fprintf(&b, "repair attempts used: %d of %d\n", s.RepairCount, s.Limits.MaxRepairAttempts)
		if s.Failure.Resumable {
			b.WriteString("resumable: yes, fix the cause and start a new run referencing this report\n")
		} else {
			b.WriteString("resumable: no\n")
		}
		b.WriteString("action required: address the failure; the generated tree, if any, is left in place and reported above\n")
	default:
		fmt.Fprintf(&b, "in progress: completed %s\n", strings.Join(stageNames(s.Completed), ", "))
		b.WriteString("action required: advance the run\n")
	}
	return b.String()
}

func stageNames(stages []Stage) []string {
	out := make([]string, 0, len(stages))
	for _, s := range stages {
		out = append(out, string(s))
	}
	return out
}
