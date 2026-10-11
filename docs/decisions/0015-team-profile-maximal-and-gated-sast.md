# ADR-0015: Scorecard-maximal team profile and gated SAST checks

- Status: accepted
- Date: 2026-10-11
- Spec: spec.md §7.6 (required code-scanning checks), §8 (security model)
- Implementation: `configs/github/team.yaml`, `configs/github/rulesets/main-team.json`, `internal/config/contexts.go`, `internal/bootstrap/plan.go`, `internal/doctor/doctor.go`, `repoforge.yaml`, `docs/architecture/github-governance.md`, `docs/reference/limitations.md`
- Implements: issue #16

## Context

The engine repository dogfooded the solo governance profile, and the
OpenSSF Scorecard Branch-Protection and Code-Review checks scored it
accordingly: zero approvals required, no code-owner review, no stale
review dismissal, no last-push approval, and no merged changeset had
ever been reviewed by a non-author. The standard shipped the same
solo shape as the template's default profile, with a test pinning it
("a single maintainer cannot supply an independent approval").

Scorecard's Branch-Protection check awards a maximal score only when
all four review settings are required: at least one approver,
stale-review dismissal, code-owner review, and last-push approval. The
team profile satisfied three of the four — it required one approval,
stale-review dismissal, and code-owner review, but not last-push
approval.

Separately, the SAST story was incomplete in a way Scorecard does not
measure but the OpenSSF Baseline does: CodeQL default setup runs on
every pull request, but its analyses are not required status checks,
so a finding never blocks a merge. OSPS-VM-06.02 requires a SAST
status check that must pass before changes merge.

## Decision

1. The team profile gains `require_last_push_approval: true`. The team
   profile is now maximal by Scorecard's Branch-Protection criterion
   in all four dimensions, and its YAML document and canonical
   ruleset payload carry the change together (`configs/rulesets_test.go`
   cross-checks them). The strict profile is unchanged: two approvals
   for its threat model, not one.
2. The solo profile is explicitly documented as below the bar by
   design. A single maintainer cannot supply a non-author approval —
   GitHub forbids approving one's own pull request — so a solo
   repository cannot reach a maximal score, and the standard now says
   so instead of leaving the gap undocumented. The template's default
   profile stays solo, pinned by its existing test.
3. When the contract requires code scanning, the desired main ruleset
   includes the CodeQL default-setup analysis contexts as required
   status checks: `Analyze (go)` for a repository with a Go module,
   `Analyze (actions)` for a repository with workflow files (default
   setup runs the actions analysis for any repository carrying
   workflows). Detection is local and deterministic, matching the
   existing context discovery. The SAST analysis therefore gates every
   merge (spec.md §7.6, OSPS-VM-06.02).
4. The engine repository adopts the team profile (`repoforge.yaml`).
   The ruleset is applied by `repoforge bootstrap` once a second
   maintainer exists; until then the declared profile is desired state
   and `doctor` reports the drift, which is the correct read-only
   behavior for a policy the operator has committed to but not yet
   applied.

## Alternatives considered

- Leaving the team profile at three of four settings and dismissing
  the Branch-Protection alert. Rejected: the fourth setting is a
  one-line config change that makes the profile genuinely maximal;
  dismissal would record acceptance of a gap that has a real fix.
- Making the engine repository strict. Rejected: two approvals cannot
  be supplied by a two-person project (the author cannot approve),
  so strict would freeze the repository. Team with last-push approval
  is the maximal shape a two-person project can satisfy.
- Deriving the CodeQL contexts from the GitHub languages API at plan
  time. Rejected for v1: the API's language names do not map 1:1 onto
  CodeQL analysis contexts (Shell is reported but never analyzed), and
  a mismatched required context blocks merges forever. Local detection
  covers the standard's languages; the limitation is documented in
  docs/reference/limitations.md.
- Requiring the code-scanning contexts unconditionally. Rejected: a
  repository whose policy does not require code scanning would then
  require checks that never report and could never merge.

## Consequences

- Team-profile repositories now meet the maximal branch-protection
  bar, and the Code-Review score climbs as reviewed changesets
  accumulate (the check is proportional over the last 30 commits).
- Every change to a code-scanning repository is gated by the SAST
  analysis, satisfying OSPS-VM-06.02 for the standard.
- `doctor` on the engine repository reports the team-ruleset drift
  until `bootstrap` applies it — honest pending state, not a silent
  divergence.
- A repository that adopts a CodeQL language beyond Go and workflows
  must extend `config.CodeScanningCheckContexts`; until it does, that
  language's analysis runs but does not gate merges. The limitation is
  documented rather than hidden.
