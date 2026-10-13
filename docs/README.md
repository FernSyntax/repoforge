# Documentation index

Entry point for the `docs/` tree. Documents are grouped by section; each link has a one-line description.

## Architecture

- [architecture/README.md](architecture/README.md) — Design documentation for the project. Documents here describe how the
- [architecture/overview.md](architecture/overview.md) — the project answers one question: how should a serious software
- [architecture/configuration-model.md](architecture/configuration-model.md) — Field reference for the project repository contract, schema
- [architecture/github-governance.md](architecture/github-governance.md) — Architecture document. Explains how the project models GitHub
- [architecture/repository-contract.md](architecture/repository-contract.md) — Architecture document. Normative behavior lives in spec.md (sections 5,
- [architecture/security.md](architecture/security.md) — How the project defends its own repository and what it promises —
- [architecture/why-two-repositories.md](architecture/why-two-repositories.md) — the project ships as two repositories with a one-way relationship.

## Decisions

- [decisions/README.md](decisions/README.md) — Architecture Decision Records for the project. A record is written before
- [decisions/0001-go-cli-and-distribution.md](decisions/0001-go-cli-and-distribution.md) — ADR-0001: Go, a single CLI, and static distribution
- [decisions/0002-declarative-policy-and-schema-pair.md](decisions/0002-declarative-policy-and-schema-pair.md) — ADR-0002: Declarative policy with a canonical YAML schema and a derived JSON twin
- [decisions/0003-ownership-boundaries.md](decisions/0003-ownership-boundaries.md) — ADR-0003: Ownership boundaries for files and repository settings
- [decisions/0004-two-repositories-and-one-way-generation.md](decisions/0004-two-repositories-and-one-way-generation.md) — ADR-0004: Two repositories, one-way generation, deterministic output
- [decisions/0005-bootstrap-converges-doctor-audits.md](decisions/0005-bootstrap-converges-doctor-audits.md) — ADR-0005: Bootstrap converges; doctor never mutates
- [decisions/0006-github-integration-and-credentials.md](decisions/0006-github-integration-and-credentials.md) — ADR-0006: GitHub integration model, credential boundaries, and what is deliberately not supported
- [decisions/0007-spec-driven-engineering-and-traceability.md](decisions/0007-spec-driven-engineering-and-traceability.md) — ADR-0007: Spec-driven engineering with enforced issue and pull-request traceability
- [decisions/0008-template-publication-and-authentication.md](decisions/0008-template-publication-and-authentication.md) — ADR-0008: Template publication with a dedicated GitHub App
- [decisions/0009-release-engineering-and-supply-chain.md](decisions/0009-release-engineering-and-supply-chain.md) — ADR-0009: Release engineering and supply-chain guarantees
- [decisions/0010-documentation-and-wiki-strategy.md](decisions/0010-documentation-and-wiki-strategy.md) — ADR-0010: Documentation and wiki strategy
- [decisions/0011-agent-proof-pr-and-issue-gates.md](decisions/0011-agent-proof-pr-and-issue-gates.md) — ADR-0011: Agent-proof pull request and issue gates
- [decisions/0012-enforced-gates-and-gh-wrappers.md](decisions/0012-enforced-gates-and-gh-wrappers.md) — ADR-0012: Enforced local gates and thin gh wrappers
- [decisions/0013-agentic-framework.md](decisions/0013-agentic-framework.md) — ADR-0013: The agentic framework — AGENTS.md, DESIGN.md, and validated agent files
- [decisions/0014-publication-privilege-separation.md](decisions/0014-publication-privilege-separation.md) — ADR-0014: Publication privilege separation — build without the credential
- [decisions/0015-team-profile-maximal-and-gated-sast.md](decisions/0015-team-profile-maximal-and-gated-sast.md) — ADR-0015: Scorecard-maximal team profile and gated SAST checks
- [decisions/0016-provider-abstraction-over-openai-compatible-gateway.md](decisions/0016-provider-abstraction-over-openai-compatible-gateway.md) — ADR-0016: Provider abstraction over the OpenAI-compatible gateway contract
- [decisions/0017-mcp-server-stdlib-subset-over-stdio.md](decisions/0017-mcp-server-stdlib-subset-over-stdio.md) — ADR-0017: MCP server as a stdlib JSON-RPC subset over stdio
- [decisions/0018-go-native-staged-orchestration.md](decisions/0018-go-native-staged-orchestration.md) — ADR-0018: Go-native staged orchestration with file checkpoints
- [decisions/0019-local-first-lexical-retrieval.md](decisions/0019-local-first-lexical-retrieval.md) — ADR-0019: Local-first lexical retrieval, embeddings deferred
- [decisions/0020-astro-product-template.md](decisions/0020-astro-product-template.md) — ADR-0020: Astro product template with engine-side deterministic build
- [decisions/0021-langgraph-orchestration-runtime.md](decisions/0021-langgraph-orchestration-runtime.md) — ADR-0021: LangGraph orchestration runtime alongside the Go runner
- [decisions/0022-stitch-design-input.md](decisions/0022-stitch-design-input.md) — ADR-0022: Stitch design input over Streamable HTTP

## Development

- [development/README.md](development/README.md) — Contributor-facing development documentation: environment setup, build,
- [development/branching.md](development/branching.md) — Part of the [Git workflow](git-workflow.md). Rule levels (invariant,
- [development/commit-conventions.md](development/commit-conventions.md) — Part of the [Git workflow](git-workflow.md). Rule levels (invariant,
- [development/getting-started.md](development/getting-started.md) — Adopt the project for a new or existing repository, end to end.
- [development/git-hooks.md](development/git-hooks.md) — the project installs a small set of local Git hooks. They exist to give
- [development/git-workflow.md](development/git-workflow.md) — Normative workflow for all work on the project repositories. This
- [development/maintainer-workflow.md](development/maintainer-workflow.md) — How work lands in the engine repository. This is the loop every
- [development/orchestration-runtime.md](development/orchestration-runtime.md) — The LangGraph journey (ADR-0021): from a client brief to a verified,
- [development/pull-requests.md](development/pull-requests.md) — Part of the [Git workflow](git-workflow.md). Rule levels (invariant,
- [development/release-process.md](development/release-process.md) — Part of the [Git workflow](git-workflow.md). Implements spec.md §14
- [development/roadmap.md](development/roadmap.md) — What the project intends to do, and what it intends not to do, over the
- [development/roles.md](development/roles.md) — Who does what in this project, what each role is required to do, and

## Operations

- [operations/README.md](operations/README.md) — Runbooks for operating the project: publishing the template, maintaining
- [operations/first-day-runbook.md](operations/first-day-runbook.md) — Bringing a repository under the project standard, and recovering one
- [operations/incident-response.md](operations/incident-response.md) — What to do when something has already gone wrong. Stop, preserve
- [operations/template-publication.md](operations/template-publication.md) — How the published template repository receives its content. The engine
- [operations/wiki-maintenance.md](operations/wiki-maintenance.md) — The GitHub wiki is generated surface. `docs/wiki/` in this repository is

## Reference

- [reference/README.md](reference/README.md) — Reference documentation: the CLI surface, the configuration model, the
- [reference/cli.md](reference/cli.md) — `repoforge` — the project CLI. Commands: `validate`, `doctor`,
- [reference/configuration.md](reference/configuration.md) — How to write `repoforge.yaml` (engine repositories) or
- [reference/github-capabilities.md](reference/github-capabilities.md) — Reference for what the governance model can rely on, per GitHub plan
- [reference/github-support-matrix.md](reference/github-support-matrix.md) — What works where, for adopters. Maintainer reference with
- [reference/limitations.md](reference/limitations.md) — What the project deliberately does not do. Each entry states the reason,

## Security

- [security/osps-baseline.md](security/osps-baseline.md) — This document records how the project satisfies each OpenSSF Baseline
- [security/release-integrity.md](security/release-integrity.md) — What makes a the project release trustworthy, what it does not

## Release notes

- [v020-baseline-audit.md](v020-baseline-audit.md) — Point-in-time planning record for issue #28. Not normative: where this
- [v020-release-readiness.md](v020-release-readiness.md) — Recorded 2026-10-12, updated after the #47/#49/#50 mergers. This
