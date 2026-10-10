# ADR-0002: Declarative policy with a canonical YAML schema and a derived JSON twin

- Status: accepted
- Date: 2026-10-10
- Supersedes: none
- Spec: spec.md §9
- Implementation: `internal/config`, `schemas/`, `scripts/check-schema-parity`

## Context

Desired repository state has to be reviewable in a diff and machine-
checkable without executing anything. That rules out expressing the
policy in the tool's own code, and it rules out an unversioned free-form
file, because a typo then becomes a silently ignored setting.

Two concrete constraints shaped the schema design. First, unknown fields
must be detectable: a policy that says `secret_scaning` instead of
`secret_scanning` must fail, not silently disable a security feature.
Second, the JSON Schema has to be usable by editors and external
validators, but humans should edit the readable form.

## Decision

The policy is a single declarative file. `repoforge.yaml` (and `.yml`
and `.json` variants) at the repository root is the primary form;
`.repoforge/config.yml` is the hidden-directory form used by generated
repositories. The canonical schema is `schemas/repoforge.schema.yaml`;
`schemas/repoforge.schema.json` is a derived translation of the same
document. Both MUST parse to identical documents, and
`scripts/check-schema-parity` enforces it.

Every schema object sets `additionalProperties: false`, and the loaders
use strict decoders (`yaml.KnownFields(true)`, `DisallowUnknownFields`)
so unknown fields are violations, not omissions. The policy declares its
own schema version in `standard.version`; any other version is rejected.
Defaults are annotations in the schema and are realised by accessor
methods, so an omitted field and an explicitly-defaulted field behave
identically.

The GitHub governance documents in `configs/github/` have their own
canonical/derived schema pair, `schemas/github-governance.schema.*`,
for the same reasons.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| JSON only, as the single form | Humans do not want to author or review JSON with comments; the YAML form is what a reviewer actually reads. |
| YAML only, no JSON Schema | Loses editor validation, external validators, and `$id`/version discipline. |
| Two independently authored schemas | Guarantees drift; the derived translation is the only arrangement that can be checked mechanically. |
| Loose parsing with warnings | A typo that disables secret scanning must not be a warning. |

## Consequences

- Every field addition or removal touches both schema files plus the
  parity check, which makes schema changes deliberate.
- Strict decoding makes the tool hostile to partial configuration,
  which is the intended failure mode.
- Governance profiles are embedded Go files, so a profile's desired
  state ships with the binary and cannot drift from the tool.

## Security, maintainability, and operational implications

- Security settings being typo-tolerant would be a vulnerability: the
  strict decoder is a security control, not a convenience.
- The parity check is a required CI gate, so a schema change that
  forgets the JSON twin fails before merge.
- Version rejection means a newer policy cannot be silently
  mis-applied by an older binary.

## Links

- Implementation: `internal/config/config.go`, `internal/config/governance.go`
- Tests: `internal/config/schema_test.go`, `internal/config/governance_test.go`,
  `internal/validate/verify_scripts_test.go`
