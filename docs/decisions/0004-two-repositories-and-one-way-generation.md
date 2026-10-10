# ADR-0004: Two repositories, one-way generation, deterministic output

- Status: accepted
- Date: 2026-10-10
- Spec: spec.md §1.4, §13
- Implementation: `internal/template`, `template/`, `.github/workflows/`

## Context

A repository standard that also publishes a starter repository has two
audiences with opposite needs. Contributors to the standard need the
implementation, its tests, and its history. Adopters need a clean
starter repository with no engine source, no development tooling, and no
confusing second product inside it.

Putting both in one repository was tried conceptually and rejected: the
engine's `go.mod`, `internal/`, and CI would either leak into the
starter or need per-path exclusion logic that is itself a source of
bugs.

## Decision

Two repositories. `FernSyntax/repoforge` (the engine) holds the CLI, the
specification, the schemas, the tests, and the canonical template source
in `template/`. `FernSyntax/repoforge-template` (the template) is
generated and holds only the generated baseline plus its own
project-facing documentation.

Generation is one-way and deterministic. `template/` is the only source
of truth for tracked template content. The builder:

1. reads and validates the source before writing anything;
2. renders only the declared placeholder set — the double-brace variable
   names listed in `internal/template` — and leaves unprovided variables
   for adoption-time resolution;
3. refuses engine implementation material — `go.mod`, `go.sum`, `*.go`,
   `Makefile`, `node_modules`, `testdata` — plus secrets, machine paths,
   hardcoded identities, and unknown placeholders;
4. stages the whole tree and publishes it by atomic rename with rollback;
5. writes `repoforge.manifest.json` recording the immutable source
   reference and a digest of every other file.

The manifest's digest is computed over the canonical encoding with the
digest field blanked, so no entry hashes itself and a rebuild from the
same inputs is byte-identical.

Template CI re-runs `repoforge template verify --require-source`, which
fetches the source commit recorded in the manifest, rebuilds, and
compares byte-for-byte. A hand-edited tree that regenerates a matching
manifest still fails, because the recorded commit cannot produce the
edited bytes.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| One repository containing both | Engine source leaks into the starter; the starter's history becomes the engine's. |
| A hand-maintained template repository | Guarantees drift, and the template then has no provenance. |
| Snapshot the engine repository and filter it | Implicit, fragile, and impossible to audit for leakage. |

## Consequences

- The template can never contain a feature the engine does not have,
  and the engine can never "fix" the template by hand.
- Every template change is an engine change plus a publication, which
  makes template updates reviewable and attributable.
- The template repository's CI is generated too, so its checks are the
  engine's checks.

## Security, maintainability, and operational implications

- Tamper-evidence comes from provenance, not trust: a divergent template
  cannot present itself as canonical.
- Publication uses a dedicated GitHub App installed on the template
  repository alone, with a token minted only inside the trusted release
  job (ADR-0009).
- Operationally, re-running a publication for an already-applied source
  version is a no-op that reports the target is synchronized.

## Links

- Implementation: `internal/template/template.go`, `internal/template/manifest.go`,
  `internal/template/verify.go`
- Tests: `internal/template/template_test.go`, `internal/template/manifest_test.go`,
  `internal/template/outpath_test.go`
- Operations: `docs/operations/template-publication.md`
