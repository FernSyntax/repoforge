// Package orchestrate is the v0.2.0 workflow boundary (ADR-0018,
// spec.md §24 FR-036): a Go-native explicit staged runner with
// file-backed checkpoints, approval gates, and bounded repair.
//
// A run advances one stage per Advance call through specify →
// validate → approve → plan → generate → verify → review → done, with
// paused and failed as explicit halted states. Every external effect
// is recorded in the completed-actions ledger, so reloading a
// checkpoint and advancing never repeats a completed action. Approvals
// bind an action to a specification digest: a changed document voids
// them. Inference enters only through caller-supplied dependency
// functions (mocked in tests); the runner itself makes no network
// calls and holds no credentials. Checkpoints must never be given
// secrets (adoption variables included): they are plain JSON on disk.
package orchestrate
