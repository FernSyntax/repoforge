# Branching

Part of the [Git workflow](git-workflow.md). Rule levels (invariant,
enforced, recommended, project-specific) are defined in spec.md.

## Rules

**B1. `main` is the only permanent branch.** [Invariant]
It is the historical record of completed changes (pull-requests.md, M5).

**B2. All changes arrive via short-lived topic branches and pull
requests. Direct pushes to `main` are not permitted.** [Enforced]
Mechanism: branch protection on `main` (configured by `repoforge
bootstrap`, audited by `repoforge doctor`) plus required status checks
and review.

**B3. Topic branch names match `<type>/<issue>-<description>`.**
[Enforced — `repoforge branch-check`, the commit-msg and pre-push hooks,
and `repoforge validate` in CI]

```
^(feat|fix|refactor|perf|test|docs|build|ci|security|chore)/[1-9][0-9]*-[a-z0-9]+(-[a-z0-9]+)*$
```

- `<type>` is one of: `feat`, `fix`, `refactor`, `perf`, `test`,
  `docs`, `build`, `ci`, `security`, `chore`.
- `<issue>` is the numeric GitHub issue number that authorizes the
  work. Every branch references an issue.
- `<description>` is lowercase kebab-case, one to five words.

Examples: `feat/12-policy-schema`, `fix/87-doctor-nil-map`,
`docs/103-release-process`.

Note: `revert` exists as a commit type but not as a branch type. A
branch that reverts uses the type of the work it performs, usually
`fix/` or `chore/`.

**B4. One branch carries one coherent purpose.** [Recommended]
If a change cannot be summarized in the branch description, it is two
branches.

**B5. Branches are short-lived.** [Recommended]
Target days, not weeks. Long-lived work is split into independently
mergeable increments.

**B6. Pre-merge history is disposable; published history is not.**
[Invariant for the published half — spec.md §6.2]

- Before a pull request is opened, a topic branch may be rebased and
  force-pushed freely; it is unpublished work.
- After a pull request is opened, address review feedback with new
  commits. Do not force-push a branch under review (see
  pull-requests.md, PR8).
- `main` and release tags are published history: never force-push,
  rebase, or amend them (spec.md §6.2).

**B7. Stay current by rebasing onto `main`, never by merging `main`
into a topic branch.** [Recommended]
Squash merge makes pre-merge history irrelevant to the record, and
merge-back bubbles only add noise.

**B8. No release branches pre-1.0.** [Project-specific]
Fixes land on `main` and ship as a new patch release
(release-process.md). Long-term maintenance branches are a post-1.0
decision requiring an ADR.

## Branch protection target state

These GitHub settings are the convergence target for bootstrap and the
audit target for doctor. They are listed here as the specification of
intent; field names must be verified against the current GitHub API
before implementation (spec.md §7.2):

- Require a pull request before merging.
- Require status checks to pass (CI is the merge gate, spec.md P9).
- Block force pushes and branch deletion on `main`.
