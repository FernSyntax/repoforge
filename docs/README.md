# Documentation

Reference documentation for RepoForge: the CLI surface, the
configuration model, the schema pair, and the GitHub capability matrix.

## Contents

- [architecture/](architecture/) — Design documentation: how the
  system is structured and why. The normative behavior contract is
  `spec.md`; architecture docs explain, they do not define.
  - [overview.md](architecture/overview.md) — System map: control
    loop, components, invariants, and trust summary.
  - [repository-contract.md](architecture/repository-contract.md) —
    The repository contract: schema artifacts, versioning,
    unknown-field detection.
  - [configuration-model.md](architecture/configuration-model.md) —
    Field-by-field reference for the policy schema (types, defaults,
    compatibility).
  - [github-governance.md](architecture/github-governance.md) — The
    declarative GitHub governance model: baseline + profiles, API
    mapping, security tri-state, capability detection.
  - [security.md](architecture/security.md) — The security baseline:
    threat model, CI trust boundaries, token permissions, third-party
    Actions, supply chain, and release artifact integrity.
  - [why-two-repositories.md](architecture/why-two-repositories.md) —
    Why the engine and the template are separate repositories with a
    one-way relationship.
- [decisions/](decisions/) — Architecture Decision Records: what was
  decided, why, and what it constrains. Append-only; a wrong decision
  is corrected by a new record.
  - [README.md](decisions/README.md) — Index of all ADRs, with status
    and spec cross-references.
  - `0001`–`0022` — Individual ADRs, numbered chronologically.
- [development/](development/) — Contributor-facing documentation:
  environment setup, build, test, and verification workflows.
  - [getting-started.md](development/getting-started.md) — Adopter
    path: create, customize, converge, work the standard way.
  - [git-workflow.md](development/git-workflow.md) — Trunk-based
    workflow overview and enforcement status.
  - [branching.md](development/branching.md) — Branch naming and
    lifetime.
  - [commit-conventions.md](development/commit-conventions.md) —
    Conventional Commits subset and commit quality.
  - [pull-requests.md](development/pull-requests.md) — PR content
    contract and merge policy.
  - [release-process.md](development/release-process.md) — SemVer,
    tagging, changelog, GitHub releases.
  - [git-hooks.md](development/git-hooks.md) — The pinned, idempotent
    developer hooks.
  - [maintainer-workflow.md](development/maintainer-workflow.md) — The
    engine contributor loop: issue to squash-merge with all gates.
  - [roles.md](development/roles.md) — The roles, their
    responsibilities, and how the project continues if any one person
    is unavailable.
  - [roadmap.md](development/roadmap.md) — What the project intends
    to do, and explicitly not to do, over the next year.
  - [orchestration-runtime.md](development/orchestration-runtime.md) —
    The orchestration journey order, design-transfer contract,
    production layer, theme behavior, `--domain`, and review mesh.
- [operations/](operations/) — Runbooks for operating RepoForge:
  publishing the template, maintaining the wiki, recovering from
  failure, and rotating credentials.
  - [template-publication.md](operations/template-publication.md) —
    Publishing the generated template, the GitHub App, and its
    credentials.
  - [wiki-maintenance.md](operations/wiki-maintenance.md) — Seeding
    and synchronising the GitHub wiki.
  - [first-day-runbook.md](operations/first-day-runbook.md) — Bringing
    a new repository under the standard, and recovering a drifted one.
  - [incident-response.md](operations/incident-response.md) — What to
    do when something has already gone wrong.
- [reference/](reference/) — Reference documentation: the CLI surface,
  the configuration model, the schema pair, and the GitHub capability
  matrix.
  - [cli.md](reference/cli.md) — Full CLI reference: commands, flags,
    defaults, exit codes, and the validation rule inventory.
  - [configuration.md](reference/configuration.md) — Adopter's guide
    to writing `repoforge.yaml`.
  - [github-capabilities.md](reference/github-capabilities.md) —
    Capability/plan matrix for the GitHub governance model: the four
    capability states, per-plan availability, detection methods,
    diagnostic contract.
  - [github-support-matrix.md](reference/github-support-matrix.md) —
    Adopter-facing capability-by-plan matrix.
  - [limitations.md](reference/limitations.md) — What RepoForge
    deliberately does not do, and the platform reasons why.
- [security/](security/) — Security documentation: the OpenSSF
  Baseline conformance statement and release integrity procedures.
  - [osps-baseline.md](security/osps-baseline.md) — OpenSSF Baseline
    Level 3 conformance statement, with evidence for each control.
  - [release-integrity.md](security/release-integrity.md) — What makes
    a RepoForge release trustworthy, what it does not promise, and how
    to verify it.
- [v020-baseline-audit.md](v020-baseline-audit.md) — Point-in-time
  planning record for the v0.2.0 baseline audit. Not normative.
- [v020-release-readiness.md](v020-release-readiness.md) — Release
  readiness assessment for v0.2.0. Not an authorization to tag or
  publish.
