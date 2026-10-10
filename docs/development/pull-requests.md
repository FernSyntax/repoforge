# Pull Requests and Merge Policy

Part of the [Git workflow](git-workflow.md). Rule levels (invariant,
enforced, recommended, project-specific) are defined in spec.md.

## Pull request content

**PR1. One pull request has one coherent purpose.** [Enforced]
Assessed at review. A PR that mixes a feature with an unrelated
refactor is sent back for splitting.

**PR2. The description contains, as separate sections:** [Enforced —
PR template + review]

1. **Summary** — what changed and why, meaningful to a reader without
   the issue's context.
2. **Validation evidence** — the verification commands run and their
   results (CONTRIBUTING.md lists the required suite). Claims without
   evidence are incomplete.
3. **Risk** — what breaks if this is wrong, and the blast radius.
4. **Security impact** — or an explicit "none".
5. **Operational impact** — effects on release, CI, or day-2
   operations; or an explicit "none".
6. **Migration impact** — what existing managed repositories or users
   must do; or an explicit "none".
7. **Breaking change declaration** — yes or no. If yes, the commits
   carry `!` and a `BREAKING CHANGE:` footer (commit-conventions.md,
   C5) and CHANGELOG.md records it.

**PR3. The title follows commit-conventions.md.** [Enforced]
Squash merge makes the PR title the commit subject on `main`.

**PR4. The PR links its authorizing issue.** [Recommended]
`Closes #123` in the description.

**PR5. The diff is reviewable in one sitting.** [Recommended]
Larger work is decomposed into stacked or sequential PRs.

**PR6. All required CI checks pass before merge.** [Enforced]
CI is authoritative (spec.md P9). A red check is never overridden,
disabled, or re-run into green without diagnosis.

**PR7. At least one approval from someone who is not the author.**
[Enforced]
The author never approves their own PR. No self-approval, no disabled
checks, no governance bypass — this half is [Invariant].

**PR8. Feedback is addressed with new commits.** [Recommended]
No force-push while under review (branching.md, B6). Squash merge
renders pre-merge commit noise harmless.

## Merge policy

**M1. Squash merge is the default and preferred method.** [Enforced —
repository setting converged by `repoforge bootstrap`, audited by
`repoforge doctor`]

**M2. Merge commits are disabled by default.** [Project-specific,
Enforced]
GitHub setting `allow_merge_commit: false`.

**M3. Rebase merges are disabled by default.** [Project-specific,
Enforced]
GitHub setting `allow_rebase_merge: false`.

**M4. The source branch is deleted after merge.** [Enforced]
GitHub setting `delete_branch_on_merge: true`. Topic branches are
disposable (branching.md, B6).

**M5. `main` is the historical record of completed changes.**
[Invariant]
Every commit on `main` is a reviewed, CI-green, complete change. There
is no "work in progress" on `main`.

**M6. No direct pushes to `main`.** [Enforced]
Branch protection target state in branching.md.

**M7. The squash commit message is curated at merge time.** [Enforced]
Merger confirms the subject equals the PR title (PR3) and the body
carries the PR summary and footers, including any `BREAKING CHANGE:`
footer.

Setting names in M2–M4 reflect the GitHub repository settings model at
the time of writing. Implementation must verify them against current
GitHub API documentation before use (spec.md §7.2); until bootstrap and
doctor exist, maintainers apply them manually and review enforces.
