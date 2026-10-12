# ADR-0018: Go-native staged orchestration with file checkpoints

- Status: proposed
- Date: 2026-10-12
- Spec: spec.md §24, FR-036 (planned)
- Implementation: `internal/orchestrate/*`
- Implements: issue #38

## Context

FR-036 needs explicit stages, persistent checkpoints, approval gates,
and bounded repair. Two runtimes are on the table: the
prompt-mandated LangGraph (Python) and a Go-native runner. The
repository is 100% Go with no Python: the schematools history shows
pip was deliberately removed (Scorecard pinned-dependencies), P11
bars unjustified runtimes, and Layer B forbids duplicating business
rules across languages without conformance tests. No Python test,
lint, or packaging infrastructure exists in CI.

## Decision

v0.2.0 orchestration is a Go-native explicit staged runner:
versioned run state (`internal/orchestrate`), atomic JSON checkpoint
files under a run directory, approval gates matched by action plus
artifact version, bounded repair loops, and inference only through an
injected function (mocked in tests). The state contract is
runtime-agnostic JSON so a future LangGraph adapter can drive the
same runs; LangGraph itself is deferred, not forked around.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Python LangGraph runtime now | New interpreter + pip + LangGraph dependency chain, new CI matrix, and cross-language duplication of validation/generation rules — cost wildly exceeds the v0.2.0 vertical slice; P10/P11 point the other way. |
| In-memory workflow state | Long runs must survive process restarts (FR-036); memory cannot. |
| SQLite checkpointer now | No SQLite driver exists in the module (P11); file checkpoints meet the single-operator v0.2.0 need with zero new dependencies. Revisit for multi-user. |
| PostgreSQL checkpointer | Enterprise-grade appearance without a multi-user requirement; explicitly deferred by the mandate itself. |

## Consequences

Deterministic, offline-testable orchestration with no new
dependencies. What hurts: Python-native LangGraph ecosystems cannot
plug in until the adapter is written, and file checkpoints assume one
operator per run directory (concurrent writers are not coordinated).

## Security, maintainability, and operational implications

- Security: checkpoints are local JSON containing no credentials
  (provider keys never enter run state); approval records bind action
  to artifact version so a stale "yes" authorizes nothing.
- Maintainability: one package owns stages, transitions, and bounds;
  maintainers must know the stage graph in the package doc.
- Operations: runs resume with `load + advance`; retention/cleanup of
  run directories is operator policy, documented in the package doc.

## Links

- Issue: #38
- Pull request: TBD
- Tests: `internal/orchestrate/*_test.go` (deterministic, injected fakes)
- Related decisions: 0001 (Go CLI, stdlib-first), 0016 (injected-boundary style)
