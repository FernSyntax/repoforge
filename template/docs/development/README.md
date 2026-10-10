# Development

Development workflow for this repository.

- [first-day.md](first-day.md) — from template to first PR, end to end
- [branch-strategy.md](branch-strategy.md) — trunk-based branching
- [commit-strategy.md](commit-strategy.md) — Conventional Commits and
  commit quality
- [pull-request-strategy.md](pull-request-strategy.md) — PR content
  contract, review, merge policy
- [release-strategy.md](release-strategy.md) — SemVer, tags, changelog,
  GitHub releases

Operational runbooks live in [../operations/](../operations/): wiki
maintenance, first-day, and incident response.

## Ground rules

- Branch from `main`: `<type>/<issue>-<description>`.
- Commits: Conventional Commits (`<type>(<scope>): <subject>`).
- Merging: squash only; the source branch is deleted on merge.
- Verify locally with `sh scripts/verify`. CI is authoritative.
