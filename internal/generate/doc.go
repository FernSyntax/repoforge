// Package generate drives the v0.2.0 journey from the CLI: it asks the
// intake questions, shows the drafted specification for a typed
// approval, runs the embedded orchestration runtime, and reports the
// outcome (spec.md §24, FR-036).
//
// The driver owns no workflow state of its own: every step is a
// subprocess call against the embedded Python runtime, so what a user
// sees is exactly what the journey runs. All input arrives through an
// injected Prompter, which makes the loop testable without a terminal
// and keeps the command's I/O explicit.
package generate
