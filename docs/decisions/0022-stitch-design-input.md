# ADR-0022: Stitch design input over Streamable HTTP

- Status: proposed
- Date: 2026-10-13
- Spec: spec.md §24 (design input; extends FR-035 tool surface outward)
- Implementation: `orchestration/stitch.py`
- Implements: issue #60

## Context

The journey needs real design input (screens, tokens, systems), and
`https://stitch.googleapis.com/mcp` exposes exactly that: 15 tools
over Streamable HTTP, verified 2026-10-13 against its live
`tools/list` (project CRUD, screen generate/edit/variants, design
systems). ADR-0017 deliberately scoped the engine's MCP surface to
local stdio and deferred remote transport; this record fires that
revisit trigger for one endpoint, not for MCP-in-general. The engine
itself stays stdio-only.

## Decision

A stdlib Streamable-HTTP JSON-RPC client lives in
`orchestration/stitch.py`: key from `STITCH_API_KEY` only, redacted
errors, classified failures (auth/rate-limit/server/timeout/invalid),
hard bounds (one project and at most two screens per run by default),
and no automatic deletion of remote state. The graph gains an
optional design branch — skipped cleanly with a recorded reason when
no key is configured — that creates a project, generates one desktop
screen from the brief, and returns tokens plus screenshot references
for downstream stages. Live calls are operator-run only; tests and CI
use recorded shapes.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Unofficial community Stitch servers | Unverified code handling credentials; the official endpoint exists, so there is no reason to trust a proxy. |
| Putting the key in repo config or chat | Pasted keys are compromised keys; env-only with rotation is the only acceptable handling. |
| Importing Stitch code output directly | Unproven mapping into our template; tokens and direction first, code import as a separate unit. |
| Auto-deleting remote projects | Destructive remote side effects need explicit operator action, not automation. |

## Consequences

Design input becomes real instead of asserted, with quota-bounded
remote calls. What hurts: a second MCP dialect (HTTP vs stdio) to
maintain, and a hard dependency on Stitch availability for that
branch — which is why the branch skips cleanly.

## Security, maintainability, and operational implications

- Security: key from env only, never logged, never committed, never
  sent anywhere except the Stitch endpoint; rotate immediately if
  ever pasted; restrict the key in console.
- Maintainability: one module owns the contract, pinned against the
  verified `tools/list`; maintainers re-verify on Stitch changes.
- Operations: without a key the branch records `design_skipped` and
  the journey continues on the local template.

## Links

- Issue: #60
- Pull request: TBD
- Tests: `orchestration/tests/test_stitch.py` (mocked HTTP, no key, no network)
- Related decisions: 0017 (deferred remote transport; trigger fired), 0021 (journey this plugs into)
