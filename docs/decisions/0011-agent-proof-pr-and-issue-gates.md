# ADR-0011: Agent-proof pull request and issue gates

- Status: accepted
- Date: 2026-10-11
- Spec: spec.md §20.2 (PR traceability), §20.3 (exceptions)
- Implementation: `internal/gitrules`, `cmd/repoforge`, `.github/workflows/pr-traceability.yml`, `AGENTS.md`

## Context

PR #4 failed its traceability check because the author (an AI agent)
wrote a PR body from memory instead of from the template: three
free-form sections instead of the ten the contract requires. The
checker worked exactly as designed — the failure was a missing
author-side gate. Humans copy the template GitHub pre-fills; API and
CLI authors get no pre-fill, so agents fail this check by default.

A second defect surfaced during recovery: the traceability workflow
reads the body from the frozen triggering event payload, so re-running
a failed run re-checks the original body forever. The corrected body
only validates on a fresh event.

## Decision

Make the template impossible to miss and the gate impossible to skip,
without adding any new required CI check:

1. `repoforge pr scaffold --issue N` prints the PR body skeleton:
   every heading from `gitrules.RequiredSections` (the checker's own
   list, so scaffold and checker agree by construction) with
   `Closes #N` pre-placed in Why and an HTML-comment prompt under each
   heading. Comments are stripped before validation, so an unfilled
   section still fails — the scaffold guides, it never passes for you.
2. `repoforge pr ready --file BODY [--path DIR]` is the single local
   gate: branch readiness (`CheckPRReady`: topic branch, commits
   ahead, commit messages, clean tree) plus the body contract
   (`CheckPRBody`, with `--repo` enabling issue-existence
   confirmation). Exit 0 means every deterministic check CI runs will
   pass. It prints exactly what is red otherwise.
3. `repoforge issue scaffold --type bug|feature|task|security` prints
   the issue-form skeleton (`### <label>` per required form field,
   derived from `.github/ISSUE_TEMPLATE/<type>.yml`), and
   `repoforge issue check --file --type TYPE` validates required
   fields are non-empty. No CI check on issue bodies is added:
   humans file through the web forms, and over-constraining them
   buys nothing. The pair exists so agents file once and file right.
4. The traceability workflow fetches the live body
   (`gh pr view --json body`) instead of the event payload, so
   re-runs and `edited` events validate what the PR says now, not
   what it said at `opened`. A fetch failure fails the run; a stale
   pass is worse than a loud error.
5. `AGENTS.md` instructs agents to run `repoforge pr ready` before
   opening a pull request — the gate only works if it is run.

## Alternatives considered

- A `repoforge pr create` wrapper that scaffolds, validates, and
  calls `gh pr create`. Rejected: it Drags credentials and network
  scope into an offline-first tool and duplicates `gh`. Scaffold +
  ready keeps repoforge credential-free on this path; the agent's
  last step stays `gh pr create --body-file`.
- A required CI check on issue bodies. Rejected: no such contract
  exists in spec.md, and mandating machine-shaped bodies from
  humans contradicts the forms' purpose.
- Duplicating the section list into the scaffold. Rejected:
  drift between two lists recreates this exact incident. The
  scaffold renders `RequiredSections`; a test pins the template
  headings against the same list (the existing forms check already
  guards template content).

## Consequences

- An agent that runs `scaffold` → fills → `ready` → `gh pr create`
  cannot produce a traceability failure. An agent that skips the
  gate can still fail CI — tooling removes the excuse, not the
  freedom. CI remains authoritative; local green covers every
  deterministic content check, never CodeQL, dependency-review,
  secret-detection, or scorecard.
- The workflow change is behavior-preserving on `opened`
  (payload body equals live body) and strictly fresher afterwards.
