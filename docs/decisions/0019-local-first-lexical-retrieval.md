# ADR-0019: Local-first lexical retrieval, embeddings deferred

- Status: accepted
- Date: 2026-10-12
- Spec: spec.md §24, FR-037 (planned)
- Implementation: `internal/retrieve/*`
- Implements: issue #40

## Context

FR-037 needs retrieval with provenance, freshness, secret exclusion,
and injection-safe handling. Candidates: a vector database, remote
embeddings through the gateway, a local embedding model, or lexical
search. The repository knowledge base is small (hundreds of files),
queries center on exact symbols, identifiers, filenames, and error
messages, and the mandate says to evaluate lexical search before
adding infrastructure — and to keep embedding generation separate
from inference quota.

## Decision

v0.2.0 retrieval is lexical and local: deterministic token scoring
with filename boost and exact-phrase bonus over line-window chunks,
content-hash freshness with added/changed/removed reporting,
path-based exclusion of secrets and artifacts, and full provenance on
every hit. Retrieved content is inert data with its source attached;
it never executes and cannot override policy (enforced by callers,
proven by test). Embeddings stay a documented extension point behind
the `Index`/`Hit` shapes, not a dependency.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Vector database (embedded or server) | Infrastructure without a demonstrated recall need at this corpus size; operational cost with no measured benefit. |
| Remote embeddings via the gateway | Spends free inference quota on plumbing and couples retrieval availability to model availability; the mandate keeps them separate. |
| Local embedding model now | Installation weight, resource requirements, and fallback complexity before lexical quality is even measured. Revisit with recall evidence. |
| No retrieval (orchestration reads files directly) | Unbounded context with no provenance, no freshness, and no secret boundary; FR-037 exists precisely to prevent this. |

## Consequences

Zero-dependency, offline, deterministic retrieval that CI can test
exactly. What hurts: paraphrase queries score poorly until embeddings
land; large generated trees need refresh discipline from callers.

## Security, maintainability, and operational implications

- Security: secret-bearing paths never enter the index; hits carry
  provenance so fabricated citations are checkable; hostile retrieved
  text is inert by construction (no execution path exists).
- Maintainability: one package owns chunking, scoring, and freshness;
  maintainers must know the walk rules and scoring weights in the
  package doc.
- Operations: indexes build in milliseconds locally and serialize to
  JSON for reuse; no service, no model download, no quota.

## Links

- Issue: #40
- Pull request: #41
- Tests: `internal/retrieve/*_test.go` (temp trees, no network)
- Related decisions: 0001 (Go CLI, stdlib-first), 0018 (orchestration consumes this)
