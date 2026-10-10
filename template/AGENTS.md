# AGENTS.md

Interoperability notes for AI coding agents working in this repository.

## Source of truth

`spec.md` is the normative product specification. If this file and
`spec.md` disagree, `spec.md` wins and this file is the bug.

## Verification

Run before declaring any task complete, and report failures honestly:

```sh
sh scripts/verify
```

Every failing check exits non-zero and names what failed. CI is the
authoritative merge gate; local checks are developer feedback and can be
bypassed, so never treat a local pass as the whole story.

## Rules

- Keep changes scoped to the task. Do not implement future work early.
- Branch as `<type>/<issue>-<description>`; commit with Conventional
  Commits; one coherent purpose per pull request.
- Every material change updates `spec.md` or says in the pull request
  why it does not.
- Major design decisions need an ADR in `docs/decisions/` first.
- Never commit credentials, tokens, machine-specific paths, or
  fabricated results. Template placeholders (double-brace syntax) are
  resolved by tooling — do not substitute identities by hand.
- Never bypass repository governance: no force-push to `main`, no
  disabling checks, no self-approving, no rewriting published history.
- Comments must explain why, not what. No stale, misleading, or
  paraphrasing comments. A `TODO` without a linked issue is a defect.
- Stop when the requested scope is complete. Report what changed and how
  it was verified.
