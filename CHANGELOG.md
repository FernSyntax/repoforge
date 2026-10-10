# Changelog

All notable changes to RepoForge are recorded in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

RepoForge v0.1.0 is the first public release. It begins a fresh product
history: there is no earlier version of *this* product to record.

## [0.1.0] — 2026-10-10

First public release.

### Added

- **Declarative repository policy.** A single validated policy file at
  `repoforge.yaml` (or `.repoforge/config.yml`), with a canonical YAML
  schema in `schemas/repoforge.schema.yaml`, a derived JSON twin, strict
  unknown-field rejection, and an explicitly declared schema version.
- **`repoforge validate`** — offline, deterministic repository
  validation covering baseline files, ownership declarations, branch and
  commit policy, GitHub YAML, action pinning, template placeholders,
  secret-shaped material, absolute machine paths, internal links, the
  ADR index, and the legacy-branding leak gate.
- **`repoforge doctor`** — strictly read-only local and remote audit.
  Every remote call is a GET; an incomplete audit is never reported as
  drift.
- **`repoforge bootstrap`** — plan, confirm, apply, re-read, verify.
  Idempotent convergence for repository settings, canonical labels, the
  two RepoForge-owned rulesets, and the security posture, with a
  dry-run that mutates nothing.
- **Governance model** — `solo`, `team`, and `strict` profiles layered
  on a shared baseline, realised as `repoforge:main` and
  `repoforge:release-tags` rulesets. Profiles require Issues and the
  wiki.
- **Template pipeline.** A deterministic builder over the canonical
  `template/` source, filesystem-safety checks, atomic publication,
  literal placeholder replacement, and a provenance manifest
  (`repoforge.manifest.json`) whose digest never covers itself.
- **`repoforge template verify`** — proves a built tree is
  internally consistent *and* reproducible from the source commit
  recorded in its own manifest.
- **`repoforge wiki sync`** — converges the GitHub wiki from
  `docs/wiki/`. Dry run by default; never deletes undeclared pages,
  never force-pushes, refuses a foreign destination, and distinguishes
  a missing wiki from an authentication failure.
- **Release engineering.** Static version-stamped binaries, SHA-256
  checksums, SPDX SBOMs, and a build-provenance attestation over the
  checksum file.
- **CI and developer tooling.** Required checks for formatting,
  `go vet`, build, tests, race tests, schema parity, template build and
  determinism, branding and leak checks, workflow and shell lint, and
  release validation. A single local entry point (`scripts/verify`) and
  pinned, idempotent developer hooks.
- **Documentation.** The normative `spec.md`, ten accepted ADRs,
  architecture, development, operations, reference, and security
  documentation, and a fill-in-ready project specification for the
  generated template.

[0.1.0]: https://github.com/FernSyntax/repoforge/releases/tag/v0.1.0
