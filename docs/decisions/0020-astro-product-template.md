# ADR-0020: Astro product template with engine-side deterministic build

- Status: proposed
- Date: 2026-10-12
- Spec: spec.md §24, FR-039 (planned)
- Implementation: `product/astro/*`, `internal/template/product.go`, `repoforge astro build`
- Implements: issue #42

## Context

FR-039 needs an Astro marketing-site product template that builds
reproducibly with provenance. The engine ships only the governance
baseline (`template/`, published to `repoforge-template`); no product
source, product builder, or publication target exists. The mandate
wants one coherent stack (Astro, strict TypeScript), no competing
styling systems, no tracking, no fictitious business identity, and
optional features kept opt-in.

## Decision

The canonical product source lives at `product/astro/`: Astro with
strict TypeScript, zero styling frameworks (design tokens in one CSS
file), zero client JavaScript, zero tracking, semantic accessible
markup, and brand carried only by required variables (`ProjectName`,
`Description`). The engine renders it through a product builder that
reuses `internal/template` primitives (variable validation,
rendering, manifest, output policy, atomic publish) with governance
checks replaced by an unresolved-placeholder gate. Publication of the
built site (new repository, workflow) is deferred to an explicit
owner decision with separate authorization; this unit proves the
build, not the destination. npm dependencies are exact-pinned with a
committed lockfile.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Tailwind by default | A second styling system with build-time weight for a template whose tokens fit in one CSS file; stays a documented opt-in. |
| Second builder duplicating render/publish logic | Duplicates business rules without conformance tests; the product path reuses the same primitives instead. |
| Publishing into `repoforge-template` | That repository must contain only the generated governance baseline (spec.md §13.1); a product site there violates the contract. |
| Choosing the publication repo here | Creating repositories and publication workflows is an owner authorization, not an agent decision; recorded as the deferred follow-up. |
| PWA, CMS, logo generator now | Maintenance cost exceeds v0.2.0 value; each is a documented opt-in. |

## Consequences

One reviewable product target with engine-proven determinism and
provenance. What hurts: `astro build` itself needs node plus a
registry fetch, so it stays operator verification (documented, run
once as evidence) rather than CI; placeholder-brand strings remain
until an adopter supplies real content.

## Security, maintainability, and operational implications

- Security: static output, no client JS, no third-party scripts, no
  analytics; required vars cannot reintroduce placeholder syntax;
  lockfile pins the supply chain.
- Maintainability: one stack, minimal dependencies; maintainers must
  know Astro basics and the builder in `internal/template/product.go`.
- Operations: `repoforge astro build` renders deterministically;
  `npm run build` in the output verifies the site; security-header
  guidance ships in the product README per deployment target.

## Links

- Issue: #42
- Pull request: TBD
- Tests: `internal/template/product_test.go`, `cmd/repoforge` astro tests (offline); `npm run build` evidence in the PR body
- Related decisions: 0002 (schema discipline for vars), 0004 (one-way generation)
