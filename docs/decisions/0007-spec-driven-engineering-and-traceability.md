# ADR-0007: Spec-driven engineering with enforced issue and pull-request traceability

- Status: accepted
- Date: 2026-10-10
- Spec: spec.md §20
- Implementation: `.github/ISSUE_TEMPLATE/`, `.github/PULL_REQUEST_TEMPLATE.md`,
  `internal/gitrules`, `.github/workflows/pr-traceability.yml`

## Context

A repository standard that is not itself disciplined is not credible.
The failure mode to avoid is a specification that drifts from the code
and a history of changes nobody can attribute to a requirement.

The constraint is GitHub's actual enforcement surface: it can require
status checks, protect refs, and validate that forms parse, but it
cannot judge whether a requirement is well written.

## Decision

`spec.md` is normative and version-controlled. Every material feature
change updates it or states in the pull request why it does not.

Work is issue-first. Bug, feature, and task issue forms each require the
fields that make the work reviewable — for bugs, observed and expected
behaviour, reproduction, severity, and the affected requirement IDs; for
features, problem, users, acceptance criteria, non-goals, spec impact,
and risks; for tasks, objective, dependencies, bounded scope, and the
parent issue. Security reports are directed to the private path and are
never collected in a public form.

Pull requests must state the problem, the linked issue, the affected
requirement IDs, the design decision, the tests, the exact validation
commands run, documentation and ADR changes, compatibility and security
effects, and known limitations. A CI check enforces linkage and the
presence of those sections, and validates that the referenced issue
exists when the event payload and token permit it.

Commit and branch naming are enforced by `repoforge git commit-check`
and `repoforge git branch-check`, by the commit-msg hook, and by CI.
Commits use a restricted Conventional Commits subset.

Exceptions are narrow: GitHub-generated dependency pull requests, and
urgent security work named as such. There is no general self-applied
bypass label. The repository initialization commit is a documented
exception because a new repository has no prior issue to reference.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Free-form issues | Fields arrive missing; triage becomes an interview. |
| A "skip process" label | A self-applied bypass is not an exception, it is an off-switch. |
| Require an ADR for every change | ADRs become noise; they are for decisions that constrain future work. |
| Enforce semantic spec quality in CI | Not reliably automatable; a heading check proves structure, not correctness. |

## Consequences

- Traceability is structural and checkable; semantic adequacy stays a
  human responsibility and is said so in the docs.
- The forms must stay valid GitHub issue-form syntax, which CI verifies.
- A small, stable label set carries priority, scope, and release target.

## Security, maintainability, and operational implications

- Security work has a private path (SECURITY.md) and never a public
  form, so an exploitable vulnerability is not disclosed by filing an
  issue.
- Requirement IDs make a removal visible: deleting an implementation
  without deleting its requirement is caught in review.
- The check is a required status check, so it cannot be bypassed by
  amending the pull request description after the fact.

## Links

- Implementation: `internal/gitrules/gitrules.go`, `internal/gitrules/prcheck.go`
- Tests: `internal/gitrules/gitrules_test.go`, `internal/gitrules/prcheck_test.go`
- Workflow: `.github/workflows/pr-traceability.yml`
