# ADR-0005: Bootstrap converges; doctor never mutates

- Status: accepted
- Date: 2026-10-10
- Spec: spec.md §10, §11
- Implementation: `internal/bootstrap`, `internal/doctor`

## Context

Two operations look similar and must not be conflated: changing a
repository to match policy, and reporting how far it is from policy.
Mixing them means a "check" can mutate a repository, which makes the
check dangerous to run in CI and destroys the operator's ability to
inspect before acting.

## Decision

`repoforge bootstrap` converges. Its cycle is plan → confirm → apply →
re-read → verify:

- Planning reads local and remote state and produces an ordered list of
  items, each classified create/update/rename/enable/disable or
  no-op/report/conflict/unsupported. Planning performs no mutation.
- `--dry-run` executes only planning and is proven by test to make no
  local change and no API call that mutates.
- Application requires `--yes` or interactive confirmation; a
  non-terminal stdin declines.
- After applying, bootstrap re-reads remote state and re-plans.
  Convergence means the second plan has no applicable and no
  unsupported item. This is what makes the tool idempotent in the
  demonstrable sense rather than the asserted one.

`repoforge doctor` audits. Every remote call it makes is a GET, asserted
by a test that fails on any non-GET method. Exit codes are 0 = no drift,
1 = drift, 2 = the audit could not complete. An incomplete audit is
never reported as drift, because blocking merges on a network failure
is worse than blocking them on nothing.

`repoforge validate` is offline and deterministic by construction: it
never touches the network, the clock, or machine-specific state.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| One command with a `--check` flag | Invites a check path that mutates; the separation is safer than the flag. |
| Doctor also fixes what it finds | Makes a read-only audit impossible to schedule safely. |
| Convergence asserted by the caller re-running | Pushes the proof onto the user; the tool proves it instead. |

## Consequences

- CI runs `validate` and can run `doctor`; neither can change the
  repository, so they are safe as required checks.
- Bootstrap is safe to re-run after a partial failure: the plan is
  recomputed from actual state, so the next run resumes rather than
  double-applies.
- The plan output is the audit trail: what changed, before and after.

## Security, maintainability, and operational implications

- Security-relevant settings are individually listed and individually
  confirmed (spec.md §8.5).
- Mutations are never retried. A transport failure after a request was
  sent reports the outcome as UNKNOWN and points the operator at
  `doctor`, because a blind retry can double-apply.
- Operationally: a partial application reports how many changes landed
  and that re-running resumes; the state is always recoverable.

## Links

- Implementation: `internal/bootstrap/bootstrap.go`, `internal/bootstrap/apply.go`,
  `internal/bootstrap/plan.go`, `internal/doctor/`
- Tests: `internal/bootstrap/bootstrap_test.go`, `internal/bootstrap/lifecycle_test.go`,
  `internal/doctor/doctor_test.go`
