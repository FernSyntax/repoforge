# ADR-0012: Enforced local gates and thin gh wrappers

- Status: accepted
- Date: 2026-10-11
- Spec: spec.md P9 (CI authoritative), §6.8 (commits), §14 (releases), §20 (traceability)
- Implementation: `cmd/repoforge`, `internal/gitrules`, `docs/development/git-hooks.md`
- Amends: ADR-0011 (reverses the `pr create` wrapper rejection)

## Context

ADR-0011 shipped scaffolds and checkers and rejected a `pr create`
wrapper as credential-scope creep that duplicates `gh`. Operation
since then showed the rejection was wrong in practice: the tools
exist, but nothing wires them into the moments they matter. Hook
installation is a script path agents never find; `gh pr create` and
`gh issue create` bypass every local gate; tagging a release runs
whatever the operator remembers. The user direction is explicit:
gate every local operation, keep CI authoritative.

## Decision

1. `repoforge hook install|verify|uninstall` is the CLI entry point
   over `scripts/hooks-install` (discovered from the repo root, not
   a memorized path). It installs nothing itself; the pre-commit
   framework still owns the blocking hooks, and foreign hooks are
   still never replaced.
2. `repoforge pr create --title T --file BODY [--draft] [--base B]`
   runs the full `pr ready` gate (branch, body, and — with `--repo`
   — issue existence), then delegates to `gh pr create`. `repoforge
   issue create --type T --title T --file BODY` validates against
   the form, applies the form's declared labels, then delegates to
   `gh issue create`. Gate-before-delegate is structural: the
   network call cannot precede a green gate in the code path.
3. `repoforge release ready --tag vX.Y.Z` runs
   `scripts/release-check.sh` as the pre-tag gate. It performs no
   tagging, pushing, or publishing — the firing action stays human.
4. Credential scope does not move: `gh` owns authentication exactly
   as before. A missing `gh` or a failed `gh auth` is reported,
   never silently skipped, and a red gate never reaches the network.
5. "Forced" is layered, honestly: hooks and wrappers catch
   violations before they leave the machine; `--no-verify` and
   raw `gh`/`git` remain possible; CI and repository rulesets stay
   the authoritative merge gates (spec.md P9). The claim is "every
   supported path is gated", never "bypass is impossible".

## Alternatives considered

- Keeping the ADR-0011 rejection and documenting raw `gh` usage.
  Rejected by the operator: documentation does not interrupt, and
  uninterrupted agents reproduce the PR #4 failure class.
- Wrappers that replicate `gh` (template selection, label
  management, draft lifecycle). Rejected: duplication rots, and
  every replicated behavior is a second contract to keep. The
  wrappers pass through `--draft`/`--base` and stop there.
- A `release create` wrapper that tags and pushes. Rejected:
  publishing side effects behind a convenience flag invert the
  release-process guarantee that pushing the tag is the single,
  deliberate firing action.

## Consequences

- The supported path for every contribution operation is gated
  locally with the same contracts CI enforces; CI turns from
  teacher into backstop.
- `git-hooks.md`, the CLI reference, AGENTS.md, and the CHANGELOG
  record the new surface; ADR-0011 stays as history with this
  amendment noted.
