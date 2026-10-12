// Package retrieve is the v0.2.0 knowledge boundary (ADR-0019,
// spec.md §24 FR-037): local-first lexical retrieval over project
// sources with source provenance on every hit.
//
// The corpus is walked under allowlist rules that exclude secrets and
// build artifacts by path; files become line-window chunks carrying
// repository, revision, path, hash, and line provenance. Search scores
// deterministically (token frequency, filename boost, exact-phrase
// bonus) so CI can assert exact rankings. Freshness is content-hash
// based: Refresh reports added, changed, and removed paths. Retrieved
// content is inert data — the package exposes no execution path — and
// cannot override policy; callers enforce that. Embeddings stay a
// documented extension point behind the Index and Hit shapes.
package retrieve
