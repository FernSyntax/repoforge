# AGENTS.md

Operating contract for AI coding agents working in this repository —
human or artificial, any platform. Read this file and
[DESIGN.md](DESIGN.md) before your first task; they are the only two
files that teach you how to work here.

Interoperability notes, not the source of project truth. If this file
and [spec.md](spec.md) disagree, spec.md wins; this file is the bug —
fix it.

## Truth hierarchy

1. [spec.md](spec.md) — normative product specification. Wins every
   disagreement.
2. [docs/decisions/](docs/decisions/) — accepted ADRs. Design history
   with reasons; [DESIGN.md](DESIGN.md) is the index to the reasoning.
3. [docs/development/](docs/development/) — normative process
   (branching, commits, PRs, releases). Extensions of spec.md.
4. This file — how to operate. Points at the above; duplicates none
   of it.

This repository was generated from a standard template; its process
docs are the strategy files under `docs/development/` (start with
[first-day.md](docs/development/first-day.md)).

## Verification

Run before declaring any task complete, and report failures honestly:

```sh
sh scripts/verify
```

Every failing check exits non-zero and names what failed. CI is the
authoritative merge gate; local checks are developer feedback and can be
bypassed, so never treat a local pass as the whole story.

## Mandatory workflows

Issue-first, always: a change without a linked issue is not a change.
Branch as `<type>/<issue>-<description>`; commit with Conventional
Commits; one coherent purpose per pull request. Fill every section of
`.github/PULL_REQUEST_TEMPLATE.md` — "none" with a reason beats an
empty section — and link the issue (`Closes #N`): CI rejects bodies
with missing sections, missing links, or empty answers.

Releases follow
[docs/development/release-strategy.md](docs/development/release-strategy.md):
tagging and publishing are external side effects, never test actions.

## Rules

- Keep changes scoped to the task. Do not implement future work early.
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
