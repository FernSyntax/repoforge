# Schemas

Machine-readable schemas (JSON Schema, draft 2020-12) for RepoForge's
declarative repository policy and related configuration files.

## Contents

- `repoforge.schema.yaml` — **canonical** repository contract
  schema, version v1. Humans edit this file.
- `repoforge.schema.json` — derived translation of the canonical
  YAML. Both files must parse to identical documents; equivalence is
  verified on every schema change (CI check planned).
- `github-governance.schema.yaml` / `.json` — canonical/derived schema
  for the GitHub governance documents in `configs/github/`
  (docs/architecture/github-governance.md).

Documentation:

- `docs/architecture/repository-contract.md` — what the contract is and
  how it is versioned.
- `docs/architecture/configuration-model.md` — field-by-field reference
  (types, defaults, compatibility notes).

## Rules

- Schemas are versioned; policy documents declare their version via
  `standard.version` and tooling rejects unknown versions (spec.md §9.2).
- Every object sets `additionalProperties: false` so unknown fields are
  detectable.
- Changes are additive within a major schema version (spec.md §16.2);
  breaking changes ship as a new schema version.
- Validation against these schemas must be deterministic (spec.md §12.2).
- No credentials, tokens, or identity values in schemas, ever.
