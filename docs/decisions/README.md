# Decisions

Architecture Decision Records for RepoForge. A record is written before
the change it describes is merged, and the decision it captures is
normative from the moment it merges.

## Index

| ADR | Title | Status | Spec |
| --- | --- | --- | --- |
| [0001](0001-go-cli-and-distribution.md) | Go, a single CLI, and static distribution | accepted | §9, §19, §16.4 |
| [0002](0002-declarative-policy-and-schema-pair.md) | Declarative policy with a canonical YAML schema and a derived JSON twin | accepted | §9 |
| [0003](0003-ownership-boundaries.md) | Ownership boundaries for files and repository settings | accepted | §5, §10.5 |
| [0004](0004-two-repositories-and-one-way-generation.md) | Two repositories, one-way generation, deterministic output | accepted | §1.4, §13 |
| [0005](0005-bootstrap-converges-doctor-audits.md) | Bootstrap converges; doctor never mutates | accepted | §10, §11 |
| [0006](0006-github-integration-and-credentials.md) | GitHub integration model, credential boundaries, and what is not supported | accepted | §7, §8, §18 |
| [0007](0007-spec-driven-engineering-and-traceability.md) | Spec-driven engineering with enforced issue and pull-request traceability | accepted | §20 |
| [0008](0008-template-publication-and-authentication.md) | Template publication with a dedicated GitHub App | accepted | §13.8, §18.4 |
| [0009](0009-release-engineering-and-supply-chain.md) | Release engineering and supply-chain guarantees | accepted | §14, §18.3 |
| [0010](0010-documentation-and-wiki-strategy.md) | Documentation and wiki strategy | accepted | §13.9, §20.6 |
| [0011](0011-agent-proof-pr-and-issue-gates.md) | Agent-proof pull request and issue gates | amended by 0012 | §20.2, §20.3 |
| [0012](0012-enforced-gates-and-gh-wrappers.md) | Enforced local gates and thin gh wrappers | accepted | P9, §6.8, §14, §20 |
| [0013](0013-agentic-framework.md) | The agentic framework — AGENTS.md, DESIGN.md, and validated agent files | accepted | §13, §20 |
| [0014](0014-publication-privilege-separation.md) | Publication privilege separation — build without the credential | accepted | §14.3.3, §18.4 |
| [0015](0015-team-profile-maximal-and-gated-sast.md) | Scorecard-maximal team profile and gated SAST checks | accepted | §7.6, §8 |
| [0016](0016-provider-abstraction-over-openai-compatible-gateway.md) | Provider abstraction over the OpenAI-compatible gateway contract | proposed | §24, FR-032–FR-034, FR-038 |
| [0019](0019-local-first-lexical-retrieval.md) | Local-first lexical retrieval, embeddings deferred | proposed | §24, FR-037 |

`spec.md` in each link is the root specification.

## How to add a record

1. Open an issue naming the decision that needs making.
2. Copy `template/docs/decisions/0000-template.md` to
   `docs/decisions/NNNN-kebab-case-title.md` with the next free number.
3. Set the status to `Proposed`, fill in every section, and open a pull
   request.
4. On merge, set the status to `Accepted` and add the row above.

## Rules

- Records are append-only. A wrong decision is corrected by a new record
  that supersedes it, never by editing the original.
- Every record lists the alternatives that were genuinely considered and
  why they lost, and states the consequences that hurt.
- Every record links the `spec.md` sections it implements, the
  implementation files, and the tests.
- An implementation detail does not need a record. A choice that
  constrains future work does.

This page is the index and is validated by CI: a missing file, a stale
row, or a broken reference fails the check.
