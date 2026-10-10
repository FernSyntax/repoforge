# Commit Conventions

Part of the [Git workflow](git-workflow.md). Rule levels (invariant,
enforced, recommended, project-specific) are defined in spec.md.

RepoForge uses a restricted subset of
[Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/).

Because squash merge is the default, the **pull request title becomes
the commit subject on `main`** — PR titles must follow this convention
(pull-requests.md, PR3).

## Format

```
<type>(<optional scope>)!: <subject>

<optional body>

<optional footers>
```

**C1. The type is one of the allowed types.** [Enforced — `repoforge
commit-check`, the commit-msg hook, and `repoforge validate` in CI]

| Type       | Use for                                                        |
| ---------- | -------------------------------------------------------------- |
| `feat`     | A new capability                                               |
| `fix`      | A bug fix                                                      |
| `docs`     | Documentation only                                             |
| `refactor` | Behavior-preserving restructuring                              |
| `perf`     | Performance improvement without behavior change                |
| `test`     | Adding or correcting tests, no production change               |
| `build`    | Build system, module, or dependency changes                    |
| `ci`       | CI configuration and scripts                                   |
| `chore`    | Routine maintenance that fits nothing above                    |
| `revert`   | Reverting a previous commit (see C7)                           |
| `security` | A security-relevant change                                     |

**C2. The subject is imperative and concise.** [Recommended]
Imperative mood ("add", not "added" or "adds"), no trailing period,
72 characters or fewer. The type and subject structure itself is
Enforced; wording style is Recommended — over-length subjects are
guidance (warning), never failures, so one long merge can never
hold `main` red.

Exempt: GitHub's synthetic test-merge commits
(`Merge <40-hex-sha> into <40-hex-sha>`), synthesized for
`pull_request` CI runs when checkout tests the merge result.
Machine-generated, never human — human merges name branches and
still fail. This exemption exists so required CI validates the
merged result instead of failing on the harness.

**C3. The scope is optional, lowercase, and names the area.**
[Recommended]
Examples: `feat(bootstrap):`, `fix(doctor):`, `docs(spec):`.

**C4. The body explains why when the reason is not obvious.**
[Recommended]
The diff already says what changed. Write the body for the future
reader who lacks your context. Wrap at 72 characters.

**C5. Breaking changes are declared twice.** [Enforced when breaking]
Append `!` after the type/scope **and** add a `BREAKING CHANGE:`
footer describing the impact and the migration path. Mirror the break
in CHANGELOG.md (spec.md §14.1).

**C6. A revert references what it reverts.** [Enforced structure]
Subject: `revert: <subject of the reverted commit>`. Body must name
the reverted commit SHA and the reason for the revert.

**C7. Security fixes under embargo do not disclose.** [Invariant]
While coordinating disclosure per SECURITY.md, commit messages and PRs
describe the change generically. Detail is published only after the
fix is released.

## Commit quality

These qualities are [Recommended], assessed at review time:

- **Atomic** — one logical change per commit. If the message needs
  "and" to describe two unrelated changes, split it.
- **Coherent** — the change, its tests, and its documentation travel
  together. A commit that breaks the build or the test suite on its
  own is not coherent.
- **Reviewable** — the diff can be understood in one sitting by the
  reviewer it is addressed to.
- **Reversible where practical** — prefer changes that can be reverted
  cleanly. Irreversible steps (destructive migrations, published
  artifacts) are called out in the body and in the PR's risk section.

## Invariants

- **No credentials, tokens, email addresses, or hardcoded identities in
  any commit content or metadata** (spec.md §8.4). [Invariant]
- **Commits are created only when the task supplies the commit message**
  — this binds automated agents and tooling, not humans writing their
  own commits (spec.md §6.3). [Invariant]
- Issue references (`Refs: #123`, `Closes #123`) appear in footers when
  the commit resolves tracked work. [Recommended]
