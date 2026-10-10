# ADR-0003: Ownership boundaries for files and repository settings

- Status: accepted
- Date: 2026-10-10
- Spec: spec.md §5, §10.5
- Implementation: `internal/config`, `internal/bootstrap`, `internal/template`

## Context

A converger that owns everything eventually overwrites something it
should not. RepoForge runs against repositories a human also works in,
and a tool that "fixes" a hand-edited workflow or deletes a file it does
not recognise is worse than no tool.

Ownership needs to be explicit, per repository, and enforceable in the
plan phase rather than discovered after a write.

## Decision

The policy declares four disjoint path sets under `ownership`:

| Set | Meaning |
| --- | --- |
| `managed_paths` | RepoForge creates and updates them; manual edits are drift. |
| `scaffolded_paths` | Created once by template or first bootstrap; the user owns them afterwards. |
| `user_owned_paths` | Tooling must never create, modify, or delete them. |
| `placeholder_paths` | Permitted to contain unresolved double-brace placeholders (the engine repository itself). |

Precedence on overlap is user-owned, then managed, then scaffolded. The
sets are validated to be disjoint and to consist of repository-relative
glob patterns; `**` matches any number of path segments including zero.

The same model applies to repository settings and rulesets. RepoForge
owns exactly two rulesets, identified by deterministic name
(`repoforge:main`, `repoforge:release-tags`) because the rulesets API
offers no metadata field. Any other ruleset is unmanaged: reported as a
conflict, never replaced, never deleted. Labels follow the same rule —
the canonical set is reconciled, everything else is preserved, and the
client deliberately has no delete method.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Own the whole repository | Destroys user work; makes adoption impossible. |
| Own nothing, only report | Gives up convergence, which is the product's point. |
| A manifest of hashes per file | Stale immediately and generates false drift on every unrelated edit. |
| Ruleset ownership by ID | IDs change on recreate; names are stable and reviewable. |

## Consequences

- Adoption is safe: a repository with unrelated content converges
  without that content being touched, and tests prove it with
  filesystem snapshots.
- RepoForge cannot "clean up" a repository it disagrees with; that is a
  deliberate limit.
- Managed files are still human-editable; the result is drift that
  `doctor` reports and `bootstrap` reconciles.

## Security, maintainability, and operational implications

- Security-relevant settings (visibility, secret scanning, rulesets) are
  individually listed and individually confirmed; visibility is never
  changed automatically at all.
- The empty bypass-actor list is sent explicitly on create and update,
  so a hand-added bypass actor is cleared by the next apply — a
  ruleset RepoForge owns is wholly owned.
- Operationally, an unmanaged conflict stops the run with a report
  naming the ruleset, so the operator decides rather than the tool.

## Links

- Implementation: `internal/config/config.go`, `internal/bootstrap/plan.go`,
  `internal/bootstrap/state.go`
- Tests: `internal/bootstrap/security_test.go`, `internal/config/glob_test.go`
