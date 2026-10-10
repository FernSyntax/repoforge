# Why Two Repositories

RepoForge ships as two repositories with a one-way relationship.
This is deliberate, and collapsing them would break the properties
each one guarantees.

## The split

- **repoforge** (engine): the CLI implementation, the policy
  specification, schemas, tests, documentation, release tooling, and
  the template builder. This is where engineering happens: issues,
  branches, PRs, CI, releases.
- **repoforge-template**: the generated baseline developers
  consume via GitHub's "Use this template". It contains policy-owned
  files rendered for a generic new repository — and nothing else.

Generation flows one way only: engine `template/` source →
`scripts/build-template` → verified output → explicitly pushed to
the template repository by a maintainer. Nothing ever flows back.

## What each side guarantees

The engine guarantees the template stays honest:

- **No implementation leakage** — tested (`template/leak` rule:
  no `.go`, no `go.mod`, no build tooling in output).
- **No identities** — tested (`template/identity`, `template/secret`,
  `template/abs-path` rules plus placeholder discipline).
- **Determinism** — tested (byte-identical rebuilds); verified again
  at publication time by `scripts/verify-template`.
- **No silent publication** — there is no automation that pushes to
  the template repository in v0.1. Every sync is a reviewed human
  action (see `docs/operations/template-publication.md`).

The template guarantees consumers a clean start:

- **Unrelated history** — GitHub generates a fresh single-commit
  history; engine commits, branches, and tags never transfer.
- **Adoption by resolution, not surgery** — placeholders from a
  closed known set, resolved once per first-day guide, then
  governed like any other repository.

## Why not one repository

- A template branch inside the engine would ship engine history to
  consumers and invite implementation code to drift into the
  baseline. Separation makes leakage structurally impossible
  instead of merely forbidden.
- Separate repositories get separate governance: the engine needs
  maintainer workflows (releases, attestations); generated projects
  need adopter workflows (features, fixes). One ruleset cannot
  serve both without exceptions, and exceptions are where
  standards rot.
- Versioning stays clean: the CLI releases (`v0.1.0`) while the
  template baseline evolves on its own cadence, regenerated —
  never hand-edited — from the engine source.
