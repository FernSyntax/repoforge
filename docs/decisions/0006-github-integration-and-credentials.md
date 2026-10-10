# ADR-0006: GitHub integration model, credential boundaries, and what is deliberately not supported

- Status: accepted
- Date: 2026-10-10
- Spec: spec.md §7, §8, §18
- Implementation: `internal/github`, `internal/wiki`

## Context

RepoForge has to administer a repository, which means holding a
credential with real power. The risk is not the feature set; it is a
token that is over-scoped, logged, persisted, or used by a workflow that
also runs untrusted code.

## Decision

**Credential sources.** Resolved at invocation time only, in priority
order: `GH_TOKEN`, `GITHUB_TOKEN`, then the authenticated `gh` session.
Nothing is written to disk, to configuration, to logs, or to fixtures.

**Redaction.** Error paths are built from fixed text; the token is never
interpolated into a message. Tests assert that a token-shaped value
never appears in output.

**Path safety.** Every API path is built by joining escaped components
through a single helper; an unsafe component (empty, `.`/`..`, a slash)
produces an error before a request is made.

**Failure classification.** Permission failures, rate limits, timeouts,
cancellation, network errors, and genuine drift are distinguished. A
mutation whose transport failed is reported as UNKNOWN, never as
success and never blind-retried.

**Wiki authentication.** `wiki sync` passes a token to git through the
`http.extraHeader` configuration entry resolved from the process
environment. It never appears in a URL, an argument, or the clone's
persisted configuration.

**Not supported, deliberately.** Organization-level settings (org
projects, org rulesets), collaborator administration, enterprise-only
ruleset rules (commit-metadata patterns, tag-name patterns), and any
visibility change. Each is either out of v1 scope or requires
administration rights the tool does not request. Capability gaps are
reported as `unsupported` findings, never silently skipped.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Persist a token in a config file | A credential at rest is a credential to leak. |
| Use a personal access token for cross-repository publication | Over-scoped and long-lived; a GitHub App scoped to one repository is strictly better (ADR-0009). |
| Retry mutations on transport failure | Double-apply is worse than an honest "unknown". |
| Silently skip unsupported settings | Hides a security gap; the operator must decide. |

## Consequences

- The tool needs administration rights for some operations and says so,
  rather than degrading quietly.
- An ambiguous mutation always ends with "re-read with `doctor`".
- Wiki sync has no credential at all in its default (planning) mode.

## Security, maintainability, and operational implications

- The attack surface is the token's scope plus the code that handles it;
  both are minimised and tested.
- Capability detection is runtime, not assumed: a feature unavailable on
  the plan surfaces as an explicit unsupported diagnostic with the plan
  requirement named.
- Rotation is a matter of replacing the environment value; nothing is
  stored to invalidate.

## Links

- Implementation: `internal/github/client.go`, `internal/github/mutations.go`,
  `internal/github/path.go`, `internal/github/auth.go`, `internal/wiki/git.go`
- Tests: `internal/github/path_test.go`, `internal/github/client_test.go`,
  `internal/github/auth_test.go`, `internal/doctor/doctor_test.go`
