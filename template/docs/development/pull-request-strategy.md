# Pull request strategy

## One coherent purpose

A PR does one thing. If the description needs "and" to cover unrelated
changes, split it.

## Required sections

The PR template is mandatory:

1. **What changed**
2. **Why** — link the issue (`Closes #123`).
3. **Validation** — commands run and their results (at minimum
   `sh scripts/verify`). Claims without evidence are incomplete.
4. **Risk** — what breaks if wrong; security impact, or "none".
5. **Operational impact** — or "none".
6. **Breaking / migration impact** — "none", or the declared break
   with `BREAKING CHANGE:` footers and a CHANGELOG.md entry.

## Title

The title follows Conventional Commits: it becomes the squash-merge
commit subject on `main`.

## Review and merge

- At least one approval from someone who is not the author. No
  self-approval.
- All required checks green. CI is authoritative; a red check is never
  overridden, disabled, or re-run into green without diagnosis.
- Conversation threads resolved before merge.
- Squash merge only (merge commits and rebase merges are disabled);
  the source branch is deleted on merge.
- CODEOWNERS changes require owner review.
