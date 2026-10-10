# Branch strategy

Trunk-based development. `main` is the only permanent branch and the
historical record of completed changes.

## Naming

```
<type>/<issue>-<description>
```

- `<type>`: `feat`, `fix`, `refactor`, `perf`, `test`, `docs`,
  `build`, `ci`, `security`, `chore`.
- `<issue>`: the numeric issue number authorizing the work.
- `<description>`: lowercase kebab-case, one to five words.

Example: `feat/12-user-login`. Validation enforces the pattern.

## Rules

- Every change arrives on a short-lived topic branch and a pull
  request. No direct pushes to `main` (enforced by ruleset).
- One branch, one coherent purpose.
- Days, not weeks: split long work into independently mergeable
  increments.
- Keep current by rebasing onto `main`, not by merging `main` into
  your branch.
- Before a PR is opened, your branch is unpublished work — rebase
  freely. After a PR is opened, answer feedback with new commits; do
  not force-push a branch under review.
- `main` and release tags are published history: never force-push,
  rebase, or amend them.

## No release branches pre-1.0

Fixes land on `main` and ship as a new patch release. Long-term
maintenance branches are a post-1.0 decision requiring an ADR.
