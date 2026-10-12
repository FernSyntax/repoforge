# Development

Contributor-facing development documentation: environment setup, build,
test, and verification workflows.

## Git and repository engineering

- [git-workflow.md](git-workflow.md) — trunk-based workflow overview and
  enforcement status
- [branching.md](branching.md) — branch naming and lifetime
- [commit-conventions.md](commit-conventions.md) — Conventional Commits
  subset and commit quality
- [pull-requests.md](pull-requests.md) — PR content contract and merge
  policy
- [release-process.md](release-process.md) — SemVer, tagging, changelog,
  GitHub releases
- [git-hooks.md](git-hooks.md) — the pinned, idempotent developer hooks
- [getting-started.md](getting-started.md) — adopter path: create,
  customize, converge, work the standard way
- [maintainer-workflow.md](maintainer-workflow.md) — the engine
  contributor loop: issue to squash-merge with all gates
- [roles.md](roles.md) — the roles, their responsibilities, and how the
  project continues if any one person is unavailable
- [roadmap.md](roadmap.md) — what the project intends to do, and
  explicitly not to do, over the next year

These documents are normative extensions of spec.md §§6, 14, and 20
(spec.md §6.10).

## Verification

The authoritative local entry point:

```sh
scripts/verify fast
scripts/verify full
```

It resolves the repository root, prints each command, propagates exit
codes, and reports a missing tool with installation instructions rather
than skipping the check. CI is authoritative; local checks are developer
feedback.
