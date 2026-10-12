# Roadmap

What RepoForge intends to do, and what it intends not to do, over the
next year. A roadmap is a statement of direction, not a commitment
schedule: items move, and that is fine — its purpose is to let users
and contributors understand where the project is going.

## Direction

RepoForge is a governance engine: a declarative policy, a deterministic
validator, a read-only auditor, an idempotent converger, and a
generated baseline repository. The direction is depth over breadth —
make the standard complete, proven, and boring for the repositories
that adopt it.

## Intended

- **Harden what ships.** v0.1.0 is the first release. The near-term
  work is the hardening pass: coverage to the OpenSSF silver and gold
  bar, the publication pipeline's trust boundaries, and the release
  checks that gate a tag.
- **Prove the standard's claims.** Every requirement in `spec.md` §22
  maps to a test; every control in the OpenSSF Baseline maps to
  evidence. The conformance statement
  (`docs/security/osps-baseline.md`) is the reference, and it stays
  true as the code changes.
- **Template maturity.** The generated repository is the product most
  users touch. Intended: routine template updates as generated pull
  requests (spec §13.8), clearer adoption paths, and documentation that
  survives contact with a first-time adopter.
- **Standard coverage.** New policy surface where the standard is
  silent and adopters keep asking: the security features GitHub ships
  (secret scanning, dependency review, private reporting) and the
  checks that gate them.
- **Documentation as a first-class artifact.** The spec stays
  normative; the derived views (README, docs/) stay in sync; the
  decisions record grows by ADR, not by folklore.

## Explicitly not intended

These are the project's non-goals (spec §4). They are listed here so
the roadmap cannot be read as promising them:

- No hosted service or SaaS. RepoForge is a CLI.
- No language-specific tooling beyond wiring the standard CI hooks;
  RepoForge is language-agnostic.
- No reimplementation of GitHub branch protection, rulesets, or
  security features; RepoForge configures and audits them.
- No management of repository content (application code, docs prose)
  beyond the baseline files the policy owns.
- No organization-wide fleet dashboards in v0.1; the engine operates
  on one repository at a time.
- No backward compatibility with other repository-management tools.

## How this roadmap changes

Roadmap changes land as ordinary pull requests against this file,
issue-first, like any other change. When a roadmap item ships, the
changelog records it and this file moves on.
