# Repository Contract

Architecture document. Normative behavior lives in spec.md (sections 5,
9–13); this document explains the design.

## What the contract is

The **repository contract** is the complete, machine-checkable statement
of what a RepoForge-managed repository must look like: its policy
file, its owned files, its GitHub settings, and the invariants binding
them. A repository is "managed" when it carries a policy document that
validates against the contract schema.

The contract has three parts:

1. **The schema** — `schemas/repoforge.schema.yaml` (canonical) and
   `schemas/repoforge.schema.json` (derived). Defines the
   declarative policy's shape per spec.md §9.
2. **The field semantics** — `docs/architecture/configuration-model.md`
   documents every field: type, requirement, default, meaning, and
   compatibility.
3. **The invariants** — spec.md §5–14 (ownership, idempotence,
   read-only audit, deterministic validation, template separation).

## The policy document

A managed repository declares its desired state in a single policy
document (spec.md §9.1). Format: YAML or JSON (both validate against the
same JSON Schema semantics). The document MUST declare
`standard.version`; tooling MUST reject unknown versions (spec.md §9.2).

The policy filename is **decided**: `repoforge.yaml` at the
repository root (`.yml`/`.json` accepted), probed first, with the
`.repoforge/config.yml` (`.yaml`/`.json`) directory form accepted
for generated projects (the template emits that form). At most one
location is used per repository. Implemented by `internal/config`.

Ownership glob semantics are **decided**: `path.Match` anchored at the
repository root, extended with `**` which matches any number of path
segments including zero. The matcher is implemented in
`internal/config` and used by `internal/validate`.

## Versioning

- The schema is versioned. Current version: **v1**.
- The version is stated three ways that must agree: the file header
  comment (`$comment`), the `$id` URN
  (`urn:repoforge:schema:repoforge:v1`), and the policy's
  required `standard.version` field, which v1 constrains to the constant
  `"v1"`.
- Evolution follows spec.md §16.2: additive within a major version, new
  enum values are additive, removals require deprecation (spec.md §15.3).
  A breaking schema change ships as `v2` alongside a new
  `standard.version` constant; v1 policies remain valid under v1
  semantics.

## Unknown-field detection

Every object in the schema sets `additionalProperties: false`. Any
field not defined by the schema version fails validation — typos are
caught, and forward-incompatible policies written for a newer schema
are rejected by older tooling (spec.md §9.2, 16.2). The merge section's
"at least one merge method" rule is expressed with `anyOf` + `const`,
which keeps `additionalProperties: false` fully effective.

`repoforge validate` will enforce this on managed repositories
(spec.md §12); until the CLI exists, any JSON Schema 2020-12 validator can
check a policy document against either schema file.

## Canonical and derived forms

- **Canonical:** `schemas/repoforge.schema.yaml`. Humans edit this
  one.
- **Derived:** `schemas/repoforge.schema.json`, produced by a
  lossless YAML→JSON translation. Both files MUST parse to identical
  documents. Equivalence is verified by `scripts/check-schema-parity` in
  the pre-commit hook and in the `Validation` workflow, and a Go test
  asserts the JSON form's structural invariants.

## What the contract does not do

- It performs no GitHub mutations. It is data. `repoforge bootstrap`
  converges to it with dry-run first; `repoforge doctor` audits it
  read-only (spec.md §10–11).
- It contains no credentials, identities, or per-owner values (spec.md §8.4). Ownership-specific values are runtime inputs, not schema
  content.
- It does not model organization-level GitHub settings (org Projects,
  org rulesets) in v1. Repository scope only (spec.md §4).
