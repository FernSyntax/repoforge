# Git Workflow

Normative workflow for all work on RepoForge repositories. This
document and its companions implement spec.md section 6 (Git model) and
section 14 (Release model). Where these documents and spec.md conflict,
spec.md wins and the document is a bug.

Companion documents:

- [branching.md](branching.md) — branch naming and lifetime
- [commit-conventions.md](commit-conventions.md) — commit message format
  and quality
- [pull-requests.md](pull-requests.md) — PR content and merge policy
- [release-process.md](release-process.md) — versioning, tagging,
  releases

## Rule classification

Every rule carries one of four levels, defined normatively in spec.md:

- **Invariant** — must always hold. A violation is a defect in the
  process or tooling, not a judgment call.
- **Enforced** — blocked in the normal flow by CI, GitHub settings, or
  required review. Bypassing requires a governance change, not
  willpower.
- **Recommended** — the default expectation. Deviation is allowed with
  a stated justification in the pull request.
- **Project-specific** — an opinion of RepoForge itself. Adopters of
  the standard may configure these differently without violating the
  model.

## Model: trunk-based development

`main` is the only permanent branch. All work flows through short-lived
topic branches and pull requests:

```
issue → branch → commits → pull request → squash merge → main → release tag
```

1. Every change starts from an issue. Branches reference it by name
   (branching.md).
2. Commits follow Conventional Commits (commit-conventions.md).
3. Pull requests carry a defined content contract and merge only
   through the merge policy (pull-requests.md).
4. Releases are cut from `main` as annotated SemVer tags with a
   changelog and a GitHub release (release-process.md).

## Enforcement status

Every rule below has a mechanism, and the mechanism is named. Where a
rule is only partially automatable, the table says which part is
human judgement.

| Rule area | Mechanism | Status |
| --- | --- | --- |
| Merge policy | `repoforge bootstrap` converges the repository settings; `repoforge doctor` audits them | Enforced |
| Branch naming | `repoforge branch-check`, the commit-msg and pre-push hooks, and `repoforge validate` in CI | Enforced |
| Commit format | `repoforge commit-check`, the commit-msg hook, and `repoforge validate` in CI; the PR title becomes the squash-merge subject | Enforced |
| PR content contract | The PR template, the `PR Traceability` workflow (issue linkage and required sections), and required review | Enforced |
| Issue linkage | `PR Traceability` validates the referenced issue against the API when the token and event payload permit | Enforced |
| Release policy | `Release Validation` and `scripts/release-check.sh` before publication; the `Release` workflow builds and publishes | Enforced |
| Traceability semantics | A human reads the requirement IDs and judges whether they are right | Human judgement — CI checks structure, not adequacy |
| Documentation adequacy | A human reads it | Human judgement |

GitHub setting names referenced anywhere in these documents must be
verified against current GitHub API documentation before relying on them
(spec.md §7.2).
